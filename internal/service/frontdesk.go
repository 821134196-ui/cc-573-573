package service

import (
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

type ReceiptLineInput struct {
	BatchItemID *uint  `json:"batch_item_id"`
	Name        string `json:"name"`
	ExpectedQty int    `json:"expected_qty"`
	ReceivedQty int    `json:"received_qty"`
	Wrong       bool   `json:"wrong"`
}

type SignInput struct {
	ReceiverName string             `json:"receiver_name"`
	SealOK       bool               `json:"seal_ok"`
	SealNote     string             `json:"seal_note"`
	Note         string             `json:"note"`
	Lines        []ReceiptLineInput `json:"lines"`
}

// Sign 企业前台签收：并列记录应收、实收与差额。
// 前台只能填实收，应收来自厨房装箱清单且不可被前台修改。
func (s *Service) Sign(actor models.User, batchID uint, in SignInput) (*models.Receipt, error) {
	if actor.Role != models.RoleFrontDesk {
		return nil, ErrForbidden
	}
	if actor.EnterpriseID == nil {
		return nil, fmt.Errorf("%w: 前台账号未绑定企业", ErrForbidden)
	}
	var rcpt *models.Receipt
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var b models.Batch
		if err := tx.First(&b, batchID).Error; err != nil {
			return ErrNotFound
		}
		if b.EnterpriseID != *actor.EnterpriseID {
			return ErrForbidden
		}
		if b.Status != models.BatchDelivered && b.Status != models.BatchResolving {
			return fmt.Errorf("%w: 批次状态 %s 不可签收（需已送达）", ErrConflict, b.Status)
		}
		a, err := s.Routes().activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if a.Status != models.AssignDelivered {
			return fmt.Errorf("%w: 当前路线任务未登记送达", ErrConflict)
		}

		var items []models.BatchItem
		tx.Where("batch_id = ?", batchID).Find(&items)
		packedByID := map[uint]models.BatchItem{}
		for _, it := range items {
			packedByID[it.ID] = it
		}

		// 每个装箱明细必须有且仅有一条正常签收行（用服务端装箱数作为应收，忽略前端篡改）
		seen := map[uint]bool{}
		var lines []models.ReceiptLine
		var shortLines []models.DiscrepancyLine
		var wrongLines []models.DiscrepancyLine
		for _, req := range in.Lines {
			if req.Wrong {
				// 错餐：实物不在装箱清单上
				if req.Name == "" || req.ReceivedQty < 0 {
					return fmt.Errorf("%w: 错餐行名称/数量非法", ErrValidation)
				}
				lines = append(lines, models.ReceiptLine{
					Name: req.Name, ExpectedQty: 0, ReceivedQty: req.ReceivedQty, Wrong: true,
				})
				if req.ReceivedQty > 0 {
					wrongLines = append(wrongLines, models.DiscrepancyLine{
						Name: req.Name, ShortQty: req.ReceivedQty, Wrong: true,
					})
				}
				continue
			}
			if req.BatchItemID == nil {
				return fmt.Errorf("%w: 正常签收行必须对应装箱明细", ErrValidation)
			}
			it, ok := packedByID[*req.BatchItemID]
			if !ok {
				return fmt.Errorf("%w: 明细不属于本批次", ErrValidation)
			}
			if seen[it.ID] {
				return fmt.Errorf("%w: 同一明细重复签收", ErrValidation)
			}
			seen[it.ID] = true
			if req.ReceivedQty < 0 {
				return fmt.Errorf("%w: 实收数量不能为负", ErrValidation)
			}
			if req.ReceivedQty > it.PackedQty {
				// 实收 > 装箱：凭空增加的餐盒，直接拒绝
				return fmt.Errorf("%w: 餐品[%s]实收%d 超过厨房装箱%d，餐盒数量不允许凭空增加",
					ErrValidation, it.Name, req.ReceivedQty, it.PackedQty)
			}
			lines = append(lines, models.ReceiptLine{
				BatchItemID: &it.ID, Name: it.Name,
				ExpectedQty: it.PackedQty, ReceivedQty: req.ReceivedQty,
			})
			if req.ReceivedQty < it.PackedQty {
				short := it.PackedQty - req.ReceivedQty
				shortLines = append(shortLines, models.DiscrepancyLine{
					BatchItemID: &it.ID, Name: it.Name,
					ExpectedQty: it.PackedQty, ReceivedQty: req.ReceivedQty, ShortQty: short,
				})
			}
		}
		if len(seen) != len(items) {
			return fmt.Errorf("%w: 签收行未覆盖全部装箱明细", ErrValidation)
		}
		if in.ReceiverName == "" {
			return fmt.Errorf("%w: 签收人必填", ErrValidation)
		}

		now := clockNow()
		r := models.Receipt{
			BatchID: batchID, AssignmentID: a.ID, Status: "signed",
			ReceiverName: in.ReceiverName, SealOK: in.SealOK, SealNote: in.SealNote,
			Note: in.Note, SignedAt: &now, CreatedAt: now,
		}
		if err := tx.Create(&r).Error; err != nil {
			return err
		}
		for i := range lines {
			lines[i].ReceiptID = r.ID
			if err := tx.Create(&lines[i]).Error; err != nil {
				return err
			}
		}

		hasDiff := len(shortLines) > 0 || len(wrongLines) > 0 || !in.SealOK
		status := models.BatchSigned
		if hasDiff {
			status = models.BatchResolving
		}
		tx.Model(&models.Batch{}).Where("id = ?", batchID).
			Updates(map[string]any{"status": status, "signed_at": now})

		// 配送任务完成（该批次在当前路线上完成；因唯一 active 索引，不可能两条路线同时完成）
		tx.Model(a).Updates(map[string]any{"active": false})

		s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: &a.RouteID,
			ActorID: actor.ID, ActorName: actor.Name, Type: "sign",
			Note: fmt.Sprintf("%s 签收，封签完好=%v", in.ReceiverName, in.SealOK),
		})
		s.addAudit(tx, batchID, actor.ID, actor.Name, "sign",
			fmt.Sprintf("签收人 %s，差异=%v", in.ReceiverName, hasDiff))

		if hasDiff {
			kind := models.DiffBroken
			switch {
			case len(shortLines) > 0 && len(wrongLines) > 0:
				kind = models.DiffShort + "+" + models.DiffWrong
			case len(wrongLines) > 0:
				kind = models.DiffWrong
			case len(shortLines) > 0:
				kind = models.DiffShort
			}
			d := models.Discrepancy{
				BatchID: batchID, RootID: b.RootID, RaisedBy: actor.ID,
				Kind: kind, Status: models.DiffOpen,
				Note: in.SealNote,
			}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
			all := append(shortLines, wrongLines...)
			for i := range all {
				all[i].DiscrepancyID = d.ID
				if err := tx.Create(&all[i]).Error; err != nil {
					return err
				}
			}
			s.addAudit(tx, batchID, actor.ID, actor.Name, "raise_diff",
				fmt.Sprintf("前台提出差异 %s，%d 行", kind, len(all)))
		}

		rcpt = &r

		// 无待处理事项则关闭整棵批次树
		if err := s.Review().maybeCloseTree(tx, b.RootID, actor); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 回填签收行返回给调用方
	if err := s.db.Preload("Lines").First(rcpt, rcpt.ID).Error; err != nil {
		return nil, err
	}
	return rcpt, nil
}

// RaiseDifference 前台在已签收批次上补充提出差异（只能新增差异单，不能改装箱数/签收数）
func (s *Service) RaiseDifference(actor models.User, batchID uint, kind string, lines []ReceiptLineInput, note string) (*models.Discrepancy, error) {
	if actor.Role != models.RoleFrontDesk {
		return nil, ErrForbidden
	}
	if actor.EnterpriseID == nil {
		return nil, ErrForbidden
	}
	var dID uint
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var b models.Batch
		if err := tx.First(&b, batchID).Error; err != nil {
			return ErrNotFound
		}
		if b.EnterpriseID != *actor.EnterpriseID {
			return ErrForbidden
		}
		if b.Status != models.BatchSigned && b.Status != models.BatchResolving {
			return fmt.Errorf("%w: 批次未签收，不能提差异", ErrConflict)
		}
		var open int64
		tx.Model(&models.Discrepancy{}).
			Where("batch_id = ? AND status = ?", batchID, models.DiffOpen).Count(&open)
		if open > 0 {
			return fmt.Errorf("%w: 该批次已有待复核差异单，请先完成复核", ErrConflict)
		}
		d := models.Discrepancy{
			BatchID: batchID, RootID: b.RootID, RaisedBy: actor.ID,
			Kind: kind, Status: models.DiffOpen, Note: note,
		}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		for _, l := range lines {
			if l.ReceivedQty < 0 {
				return fmt.Errorf("%w: 数量非法", ErrValidation)
			}
			dl := models.DiscrepancyLine{
				DiscrepancyID: d.ID, BatchItemID: l.BatchItemID, Name: l.Name,
				ExpectedQty: l.ExpectedQty, ReceivedQty: l.ReceivedQty,
				ShortQty: l.ExpectedQty - l.ReceivedQty, Wrong: l.Wrong,
			}
			if dl.ShortQty < 0 {
				return fmt.Errorf("%w: 差异数量不能为负", ErrValidation)
			}
			if err := tx.Create(&dl).Error; err != nil {
				return err
			}
		}
		s.addAudit(tx, batchID, actor.ID, actor.Name, "raise_diff", note)
		dID = d.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	var d models.Discrepancy
	s.db.Preload("Lines").First(&d, dID)
	return &d, nil
}
