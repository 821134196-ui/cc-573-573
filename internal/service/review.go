package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

type ReviewService struct{ s *Service }

func (s *Service) Review() *ReviewService { return &ReviewService{s: s} }

type ReviewLineInput struct {
	LineID      uint `json:"line_id"`
	ResupplyQty int  `json:"resupply_qty"`
	LossQty     int  `json:"loss_qty"`
	RefundQty   int  `json:"refund_qty"`
}

type ApproveInput struct {
	Lines       []ReviewLineInput `json:"lines"`
	Note        string            `json:"note"`
	AutoPostPay bool              `json:"auto_post_pay"` // 模拟支付通道立即回执入账
}

type ApproveResult struct {
	Resolution      *models.Resolution   `json:"resolution"`
	ResupplyBatchID *uint                `json:"resupply_batch_id"`
	Refund          *models.RefundRecord `json:"refund,omitempty"`
	Conservation    *ConservationReport  `json:"conservation"`
}

// Approve 复核批准差异：
//  1. 每行必须 补送+损失+退款 == 短少数（错餐行除外：只能退回，不涉及数量分配）；
//  2. 补送从原批次发起子批次（draft，由厨房补餐封箱后沿同一路线体系配送），不另建无关订单；
//  3. 退款按单价计算份数金额，经模拟支付通道入账且幂等；
//  4. 完成后立即跑整棵树数量守恒校验。
func (r *ReviewService) Approve(actor models.User, discrepancyID uint, in ApproveInput) (*ApproveResult, error) {
	if actor.Role != models.RoleReviewer && actor.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	res := &ApproveResult{}
	err := r.s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if err := tx.First(&d, discrepancyID).Error; err != nil {
			return ErrNotFound
		}
		if d.Status != models.DiffOpen {
			return fmt.Errorf("%w: 差异单已处理(%s)", ErrConflict, d.Status)
		}
		var lines []models.DiscrepancyLine
		tx.Where("discrepancy_id = ?", d.ID).Find(&lines)
		reqByID := map[uint]ReviewLineInput{}
		for _, req := range in.Lines {
			if _, dup := reqByID[req.LineID]; dup {
				return fmt.Errorf("%w: 差异行 %d 重复提交", ErrValidation, req.LineID)
			}
			reqByID[req.LineID] = req
		}

		resupplyItems := map[uint]int{} // batchItemID(原明细) -> 批准补送份数
		refundAmount := 0
		refundQty := 0

		// 每一条差异行都必须给出结论，防止短少数量挂空
		for i := range lines {
			l := &lines[i]
			req, ok := reqByID[l.ID]
			if !ok {
				if l.Wrong {
					req = ReviewLineInput{LineID: l.ID} // 错餐行默认退回
				} else {
					return fmt.Errorf("%w: 餐品[%s]短少%d 未给出补送/损失/退款分配",
						ErrValidation, l.Name, l.ShortQty)
				}
			}
			if req.ResupplyQty < 0 || req.LossQty < 0 || req.RefundQty < 0 {
				return fmt.Errorf("%w: 分配数量不能为负", ErrValidation)
			}
			if l.Wrong {
				// 错餐：批准 = 实物退回厨房，不允许借错餐行产生补送/退款
				if req.ResupplyQty+req.LossQty+req.RefundQty != 0 {
					return fmt.Errorf("%w: 错餐行[%s]只能退回，不能分配补送/损失/退款", ErrValidation, l.Name)
				}
				continue
			}
			if req.ResupplyQty+req.LossQty+req.RefundQty != l.ShortQty {
				return fmt.Errorf("%w: 餐品[%s]短少%d，补送%d+损失%d+退款%d 与短少数不一致",
					ErrValidation, l.Name, l.ShortQty,
					req.ResupplyQty, req.LossQty, req.RefundQty)
			}
			l.ResupplyQty = req.ResupplyQty
			l.LossQty = req.LossQty
			l.RefundQty = req.RefundQty
			if err := tx.Model(l).Updates(map[string]any{
				"resupply_qty": req.ResupplyQty,
				"loss_qty":     req.LossQty,
				"refund_qty":   req.RefundQty,
			}).Error; err != nil {
				return err
			}
			if req.ResupplyQty > 0 {
				if l.BatchItemID == nil {
					return fmt.Errorf("%w: 补送行缺少原装箱明细依据", ErrValidation)
				}
				resupplyItems[*l.BatchItemID] += req.ResupplyQty
			}
			if req.RefundQty > 0 {
				if l.BatchItemID == nil {
					return fmt.Errorf("%w: 退款行缺少原装箱明细依据", ErrValidation)
				}
				var bi models.BatchItem
				if err := tx.First(&bi, *l.BatchItemID).Error; err != nil {
					return err
				}
				refundAmount += req.RefundQty * bi.UnitPrice
				refundQty += req.RefundQty
			}
		}

		now := clockNow()
		if err := tx.Model(&d).Updates(map[string]any{
			"status": models.DiffApproved, "review_note": in.Note,
			"reviewed_by": actor.ID, "reviewed_at": now,
		}).Error; err != nil {
			return err
		}

		var parent models.Batch
		if err := tx.First(&parent, d.BatchID).Error; err != nil {
			return err
		}

		// Resolution 记录（一张差异单至多一条）
		res0 := models.Resolution{
			DiscrepancyID: d.ID, ReviewerID: actor.ID, Note: in.Note,
			RefundAmount: refundAmount, CreatedAt: now,
		}
		if err := tx.Create(&res0).Error; err != nil {
			return err
		}

		// 补送子批次：挂在原批次树下，保留来源差异与数量依据，不另建无关订单
		if len(resupplyItems) > 0 {
			var seq int64
			tx.Model(&models.Batch{}).Where("root_id = ?", parent.RootID).Count(&seq)
			child := models.Batch{
				EnterpriseID:        parent.EnterpriseID,
				MealDate:            parent.MealDate,
				MealType:            parent.MealType,
				RootID:              parent.RootID,
				ParentID:            &parent.ID,
				SourceDiscrepancyID: &d.ID,
				ResupplySeq:         int(seq),
				Status:              models.BatchDraft,
				CreatedBy:           actor.ID,
				Note:                fmt.Sprintf("差异单#%d 批准补送", d.ID),
			}
			if err := tx.Create(&child).Error; err != nil {
				return err
			}
			child.Code = fmt.Sprintf("%s-R%02d-%04d", parent.Code, seq, child.ID)
			for biID, qty := range resupplyItems {
				var bi models.BatchItem
				if err := tx.First(&bi, biID).Error; err != nil {
					return err
				}
				if err := tx.Create(&models.BatchItem{
					BatchID: child.ID, Name: bi.Name, UnitPrice: bi.UnitPrice, PackedQty: qty,
				}).Error; err != nil {
					return err
				}
			}
			if err := r.s.recomputeTotals(tx, child.ID); err != nil {
				return err
			}
			tx.Model(&child).Update("code", child.Code)
			res0.ResupplyBatchID = &child.ID
			tx.Model(&res0).Update("resupply_batch_id", child.ID)
			res.ResupplyBatchID = &child.ID
			r.s.addAudit(tx, child.ID, actor.ID, actor.Name, "resupply_create",
				fmt.Sprintf("差异单#%d 批准补送 %d 份", d.ID, child.PackedTotal))
			r.s.addAudit(tx, parent.ID, actor.ID, actor.Name, "review_approve",
				fmt.Sprintf("差异#%d 批准，发起补送批次#%d", d.ID, child.ID))
		} else {
			r.s.addAudit(tx, parent.ID, actor.ID, actor.Name, "review_approve",
				fmt.Sprintf("差异#%d 批准（无补送）", d.ID))
		}

		// 退款：模拟支付通道。同一 Resolution 幂等键，重复回执只入账一次。
		if refundAmount > 0 {
			idemKey := fmt.Sprintf("refund-resolution-%d", res0.ID)
			rec, err := r.s.Pay.createTx(tx, idemKey, parent.RootID, res0.ID, refundAmount, refundQty)
			if err != nil {
				return err
			}
			if in.AutoPostPay {
				rec.Status = "posted"
				rec.GatewayReceipt = fmt.Sprintf("GW-RFD-%d", res0.ID)
				rec.PostedAt = &now
				if err := tx.Save(rec).Error; err != nil {
					return err
				}
			}
			res.Refund = rec
		}

		res.Resolution = &res0
		// 事务内立即核对整棵树：不平衡则整笔回滚，绝不留下账实不符
		rep, err := r.s.checkConservationDB(tx, parent.RootID)
		if err != nil {
			return err
		}
		if !rep.Balanced {
			return errors.New(rep.Detail)
		}
		if err := r.maybeCloseTree(tx, parent.RootID, actor); err != nil {
			return err
		}
		rep2, err := r.s.checkConservationDB(tx, parent.RootID)
		if err != nil {
			return err
		}
		res.Conservation = rep2
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Reject 复核驳回：短少数成为争议数量，餐盒仍有主（不得进入退款/损失）
func (r *ReviewService) Reject(actor models.User, discrepancyID uint, note string) error {
	if actor.Role != models.RoleReviewer && actor.Role != models.RoleAdmin {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		var d models.Discrepancy
		if err := tx.First(&d, discrepancyID).Error; err != nil {
			return ErrNotFound
		}
		if d.Status != models.DiffOpen {
			return fmt.Errorf("%w: 差异单已处理", ErrConflict)
		}
		now := clockNow()
		if err := tx.Model(&d).Updates(map[string]any{
			"status": models.DiffRejected, "review_note": note,
			"reviewed_by": actor.ID, "reviewed_at": now,
		}).Error; err != nil {
			return err
		}
		r.s.addAudit(tx, d.BatchID, actor.ID, actor.Name, "review_reject", note)
		var parent models.Batch
		if err := tx.First(&models.Batch{}, d.BatchID).Error; err == nil {
			return r.maybeCloseTree(tx, parent.RootID, actor)
		}
		return nil
	})
}

// PostRefundReceipt 支付通道回执入账（幂等：重复回执只入账一次）
func (r *ReviewService) PostRefundReceipt(resolutionID uint, gatewayReceipt string) (*models.RefundRecord, error) {
	var res0 models.Resolution
	if err := r.s.db.First(&res0, resolutionID).Error; err != nil {
		return nil, ErrNotFound
	}
	idemKey := fmt.Sprintf("refund-resolution-%d", res0.ID)
	return r.s.Pay.PostReceipt(idemKey, gatewayReceipt)
}

// maybeCloseTree 当整棵树没有在途/待封箱批次且无待处理差异时关闭
func (r *ReviewService) maybeCloseTree(tx *gorm.DB, rootID uint, actor models.User) error {
	var pendingBatches int64
	tx.Model(&models.Batch{}).
		Where("root_id = ? AND status NOT IN ?", rootID,
			[]string{models.BatchSigned, models.BatchResolving, models.BatchClosed}).
		Count(&pendingBatches)
	var openDiffs int64
	tx.Model(&models.Discrepancy{}).
		Where("root_id = ? AND status = ?", rootID, models.DiffOpen).Count(&openDiffs)
	if pendingBatches == 0 && openDiffs == 0 {
		now := clockNow()
		tx.Model(&models.Batch{}).Where("root_id = ?", rootID).
			Updates(map[string]any{"status": models.BatchClosed, "closed_at": now})
		r.s.addAudit(tx, rootID, actor.ID, actor.Name, "tree_close",
			"全部签收且差异结清，批次树关闭")
	}
	return nil
}

func repRoot(child *uint, discID uint, s *Service) uint {
	if child != nil {
		var b models.Batch
		if s.db.First(&b, *child).Error == nil {
			return b.RootID
		}
	}
	var d models.Discrepancy
	if s.db.First(&d, discID).Error == nil {
		return d.RootID
	}
	return 0
}

func (p *PaymentService) createTx(tx *gorm.DB, idemKey string, rootBatchID, resolutionID uint, amount, qty int) (*models.RefundRecord, error) {
	var rec models.RefundRecord
	err := tx.Where("idem_key = ?", idemKey).First(&rec).Error
	if err == nil {
		return &rec, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	rec = models.RefundRecord{
		ResolutionID: &resolutionID, BatchID: rootBatchID,
		IdemKey: idemKey, Amount: amount, Qty: qty, Status: "pending",
	}
	if err := tx.Create(&rec).Error; err != nil {
		return nil, err
	}
	return &rec, nil
}
