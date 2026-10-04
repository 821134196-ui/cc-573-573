package service

import (
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

type ItemInput struct {
	Name      string `json:"name"`
	UnitPrice int    `json:"unit_price"` // 分
	PackedQty int    `json:"packed_qty"`
}

type CreateBatchInput struct {
	EnterpriseID uint        `json:"enterprise_id"`
	MealDate     string      `json:"meal_date"`
	MealType     string      `json:"meal_type"`
	Items        []ItemInput `json:"items"`
	Note         string      `json:"note"`
}

// CreateBatch 厨房按企业+餐次生成装箱批次（草稿，可编辑装箱数量）
func (s *Service) CreateBatch(actor models.User, in CreateBatchInput) (*models.Batch, error) {
	if actor.Role != models.RoleKitchen {
		return nil, ErrForbidden
	}
	if in.EnterpriseID == 0 || in.MealDate == "" || in.MealType == "" {
		return nil, fmt.Errorf("%w: 企业、餐次日期与类型必填", ErrValidation)
	}
	if len(in.MealDate) != 10 || in.MealDate[4] != '-' || in.MealDate[7] != '-' {
		return nil, fmt.Errorf("%w: 餐次日期需为 YYYY-MM-DD", ErrValidation)
	}
	switch in.MealType {
	case "breakfast", "lunch", "dinner":
	default:
		return nil, fmt.Errorf("%w: 餐次类型非法", ErrValidation)
	}
	if len(in.Items) == 0 {
		return nil, fmt.Errorf("%w: 装箱明细不能为空", ErrValidation)
	}
	for _, it := range in.Items {
		if it.Name == "" || it.PackedQty < 0 || it.UnitPrice < 0 {
			return nil, fmt.Errorf("%w: 明细名称/数量/单价不合法", ErrValidation)
		}
	}
	var ent models.Enterprise
	if err := s.db.First(&ent, in.EnterpriseID).Error; err != nil {
		return nil, fmt.Errorf("%w: 企业不存在", ErrValidation)
	}
	var n int64
	s.db.Model(&models.Batch{}).
		Where("enterprise_id = ? AND meal_date = ? AND meal_type = ? AND parent_id IS NULL",
			in.EnterpriseID, in.MealDate, in.MealType).Count(&n)
	if n > 0 {
		return nil, fmt.Errorf("%w: 该企业本餐次已存在原批次，少餐请走补送而非新建", ErrConflict)
	}

	b := models.Batch{
		EnterpriseID: in.EnterpriseID, MealDate: in.MealDate, MealType: in.MealType,
		Status: models.BatchDraft, CreatedBy: actor.ID, Note: in.Note,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&b).Error; err != nil {
			return err
		}
		b.RootID = b.ID // 根批次指向自己；补送批次沿用同一 RootID
		if err := tx.Model(&b).Update("root_id", b.ID).Error; err != nil {
			return err
		}
		b.Code = fmt.Sprintf("B%s%s-%04d", in.MealDate[5:], mealCode(in.MealType), b.ID)
		for i := range in.Items {
			in.Items[i].Name = trimName(in.Items[i].Name)
		}
		for _, it := range in.Items {
			if err := tx.Create(&models.BatchItem{
				BatchID: b.ID, Name: it.Name, UnitPrice: it.UnitPrice, PackedQty: it.PackedQty,
			}).Error; err != nil {
				return err
			}
		}
		if err := s.recomputeTotals(tx, b.ID); err != nil {
			return err
		}
		if err := tx.Model(&b).Update("code", b.Code).Error; err != nil {
			return err
		}
		s.addAudit(tx, b.ID, actor.ID, actor.Name, "create", "厨房生成装箱清单")
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.reloadBatch(b.ID)
}

// UpdatePacked 修改装箱数量：仅厨房、仅草稿状态。封箱后任何人都不能改。
func (s *Service) UpdatePacked(actor models.User, batchID uint, items []ItemInput) (*models.Batch, error) {
	if actor.Role != models.RoleKitchen {
		return nil, ErrForbidden
	}
	var b models.Batch
	if err := s.db.First(&b, batchID).Error; err != nil {
		return nil, ErrNotFound
	}
	if b.Status != models.BatchDraft {
		return nil, ErrSealed
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("batch_id = ?", batchID).Delete(&models.BatchItem{}).Error; err != nil {
			return err
		}
		for _, it := range items {
			if it.Name == "" || it.PackedQty < 0 || it.UnitPrice < 0 {
				return fmt.Errorf("%w: 明细名称/数量/单价不合法", ErrValidation)
			}
			if err := tx.Create(&models.BatchItem{
				BatchID: batchID, Name: trimName(it.Name), UnitPrice: it.UnitPrice, PackedQty: it.PackedQty,
			}).Error; err != nil {
				return err
			}
		}
		if err := s.recomputeTotals(tx, batchID); err != nil {
			return err
		}
		s.addAudit(tx, batchID, actor.ID, actor.Name, "pack_update", "厨房更新装箱数量")
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.reloadBatch(batchID)
}

type SealInput struct {
	// 封签号：厨房逐箱登记；数量必须与装箱餐盒所需保温箱一致说明
	SealNumbers []string `json:"seal_numbers"`
	BoxCapacity int      `json:"box_capacity"` // 每箱餐盒数；不传则每箱一封
	Note        string   `json:"note"`
}

// Seal 封箱：按封签号登记保温箱，封箱后装箱数冻结
func (s *Service) Seal(actor models.User, batchID uint, in SealInput) (*models.Batch, error) {
	if actor.Role != models.RoleKitchen {
		return nil, ErrForbidden
	}
	var b models.Batch
	if err := s.db.First(&b, batchID).Error; err != nil {
		return nil, ErrNotFound
	}
	if b.Status != models.BatchDraft {
		return nil, fmt.Errorf("%w: 仅草稿批次可封箱", ErrConflict)
	}
	if b.PackedTotal <= 0 {
		return nil, fmt.Errorf("%w: 装箱数量为 0，不能封箱", ErrValidation)
	}
	if len(in.SealNumbers) == 0 {
		return nil, fmt.Errorf("%w: 至少登记一个封签号", ErrValidation)
	}
	seen := map[string]bool{}
	for _, sn := range in.SealNumbers {
		sn = trimName(sn)
		if sn == "" || seen[sn] {
			return nil, fmt.Errorf("%w: 封签号为空或重复", ErrValidation)
		}
		seen[sn] = true
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		now := clockNow()
		for i, sn := range in.SealNumbers {
			box := models.Box{
				BatchID: batchID,
				Code:    fmt.Sprintf("%s-BX%02d", b.Code, i+1),
				SealNo:  sn, Status: "intact", SealedAt: now,
			}
			if err := tx.Create(&box).Error; err != nil {
				return fmt.Errorf("%w: 封签号 %s 已被使用", ErrConflict, sn)
			}
		}
		b2 := map[string]any{"status": models.BatchSealed, "sealed_at": now}
		if err := tx.Model(&models.Batch{}).Where("id = ? AND status = ?", batchID, models.BatchDraft).
			Updates(b2).Error; err != nil {
			return err
		}
		// 封箱闸门：整棵树必须守恒。补送批次若被加成超过复核批准份数，在此拦截。
		rep, err := s.checkConservationDB(tx, b.RootID)
		if err != nil {
			return err
		}
		if !rep.Balanced {
			return fmt.Errorf("%w: 封箱被数量守恒校验拦截（%s）", ErrConflict, rep.Detail)
		}
		s.addAudit(tx, batchID, actor.ID, actor.Name, "seal",
			fmt.Sprintf("封箱并登记 %d 枚封签: %v", len(in.SealNumbers), in.SealNumbers))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.reloadBatch(batchID)
}

func (s *Service) reloadBatch(id uint) (*models.Batch, error) {
	var b models.Batch
	err := s.db.Preload("Items").Preload("Boxes").Preload("Enterprise").First(&b, id).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func mealCode(t string) string {
	switch t {
	case "breakfast":
		return "BF"
	case "lunch":
		return "LU"
	case "dinner":
		return "DN"
	}
	return "XX"
}

func trimName(s string) string {
	var b []byte
	for _, c := range []byte(s) {
		if c == ' ' && (len(b) == 0 || b[len(b)-1] == ' ') {
			continue
		}
		b = append(b, c)
	}
	for len(b) > 0 && b[len(b)-1] == ' ' {
		b = b[:len(b)-1]
	}
	return string(b)
}
