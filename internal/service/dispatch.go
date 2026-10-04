package service

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"mealcheck/internal/models"
)

type RouteService struct{ s *Service }

func (s *Service) Routes() *RouteService { return &RouteService{s: s} }

// Assign 调度把已封箱批次指派给路线。批次同一时刻只允许一条有效路线。
func (r *RouteService) Assign(actor models.User, batchID, routeID uint, note string) (*models.RouteAssignment, error) {
	if actor.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	var a *models.RouteAssignment
	err := r.s.db.Transaction(func(tx *gorm.DB) error {
		var b models.Batch
		if err := tx.First(&b, batchID).Error; err != nil {
			return ErrNotFound
		}
		if b.Status != models.BatchSealed {
			return fmt.Errorf("%w: 当前状态 %s 不可指派路线（仅已封箱批次可指派）", ErrConflict, b.Status)
		}
		var route models.Route
		if err := tx.First(&route, routeID).Error; err != nil {
			return fmt.Errorf("%w: 路线不存在", ErrValidation)
		}
		var active int64
		tx.Model(&models.RouteAssignment{}).Where("batch_id = ? AND active = ?", batchID, true).Count(&active)
		if active > 0 {
			return fmt.Errorf("%w: 该批次已有在途路线，请走改派", ErrConflict)
		}
		var seq int64
		tx.Model(&models.RouteAssignment{}).Where("batch_id = ?", batchID).Count(&seq)
		now := clockNow()
		a = &models.RouteAssignment{
			BatchID: batchID, RouteID: routeID, CourierID: route.CourierID,
			Seq: int(seq) + 1, Status: models.AssignAssigned, Active: true, HandoverNote: note,
		}
		if err := tx.Create(a).Error; err != nil {
			// 部分唯一索引兜底：同批次两条 active 直接失败
			return fmt.Errorf("%w: 该批次已存在有效路线任务", ErrConflict)
		}
		upd := map[string]any{"status": models.BatchAssigned, "assigned_at": now}
		tx.Model(&models.Batch{}).Where("id = ?", batchID).Updates(upd)
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: &routeID,
			ActorID: actor.ID, ActorName: actor.Name, Type: "assign",
			Note: fmt.Sprintf("调度指派至路线「%s」 %s", route.Name, note),
		})
		r.s.addAudit(tx, batchID, actor.ID, actor.Name, "route_assign",
			fmt.Sprintf("路线「%s」", route.Name))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Accept 配送员接收批次（记录交接）
func (r *RouteService) Accept(actor models.User, batchID uint, note string) error {
	if actor.Role != models.RoleCourier {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		a, err := r.activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if a.CourierID != actor.ID {
			return fmt.Errorf("%w: 该路线由其他配送员负责", ErrForbidden)
		}
		if a.Status != models.AssignAssigned {
			return fmt.Errorf("%w: 任务状态 %s 不可接收", ErrConflict, a.Status)
		}
		now := clockNow()
		if err := tx.Model(a).Updates(map[string]any{
			"status": models.AssignAccepted, "accepted_at": now, "handover_note": note,
		}).Error; err != nil {
			return err
		}
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: ptrU(a.RouteID),
			ActorID: actor.ID, ActorName: actor.Name, Type: "accept", Note: note,
		})
		return nil
	})
}

// Depart 配送员出发
func (r *RouteService) Depart(actor models.User, batchID uint, note string) error {
	if actor.Role != models.RoleCourier {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		a, err := r.activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if a.CourierID != actor.ID {
			return fmt.Errorf("%w: 非当前路线配送员", ErrForbidden)
		}
		if a.Status != models.AssignAccepted {
			return fmt.Errorf("%w: 任务状态 %s 不可出发", ErrConflict, a.Status)
		}
		now := clockNow()
		tx.Model(a).Updates(map[string]any{"status": models.AssignInTransit, "departed_at": now})
		tx.Model(&models.Batch{}).Where("id = ?", batchID).Update("status", models.BatchInTransit)
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: ptrU(a.RouteID),
			ActorID: actor.ID, ActorName: actor.Name, Type: "depart", Note: note,
		})
		return nil
	})
}

// ReportBrokenSeal 途中发现封签破损：登记封签与经过，批次转差异处理，仍保持唯一在途任务
func (r *RouteService) ReportBrokenSeal(actor models.User, batchID uint, sealNo, note string) error {
	if actor.Role != models.RoleCourier && actor.Role != models.RoleFrontDesk {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		var b models.Batch
		if err := tx.First(&b, batchID).Error; err != nil {
			return ErrNotFound
		}
		var box models.Box
		if err := tx.Where("batch_id = ? AND seal_no = ?", batchID, sealNo).First(&box).Error; err != nil {
			return fmt.Errorf("%w: 封签号 %s 不在本批次", ErrValidation, sealNo)
		}
		if box.Status == "broken" {
			return fmt.Errorf("%w: 该封签已登记破损", ErrConflict)
		}
		now := clockNow()
		tx.Model(&box).Updates(map[string]any{"status": "broken", "broken_at": now, "broken_note": note})
		a, _ := r.activeTx(tx, batchID)
		var aid uint
		var routeOf *uint
		if a != nil {
			aid = a.ID
			rid := a.RouteID
			routeOf = &rid
		}
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: aid, ToRouteID: routeOf,
			ActorID: actor.ID, ActorName: actor.Name, Type: "seal_broken",
			Note: fmt.Sprintf("封签 %s 破损：%s", sealNo, note),
		})
		r.s.addAudit(tx, batchID, actor.ID, actor.Name, "seal_broken",
			fmt.Sprintf("封签 %s：%s", sealNo, note))
		// 同时登记一条封签破损差异，复核闭环
		d := models.Discrepancy{
			BatchID: batchID, RootID: b.RootID, RaisedBy: actor.ID,
			Kind: models.DiffBroken, Status: models.DiffOpen,
			Note: fmt.Sprintf("封签 %s 破损：%s", sealNo, note),
		}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		if b.Status == models.BatchInTransit || b.Status == models.BatchDelivered {
			tx.Model(&b).Update("status", models.BatchResolving)
		}
		return nil
	})
}

// Reassign 途中改派：旧任务关闭（reassigned），新任务生效；同一时刻仍只有一条有效路线。
func (r *RouteService) Reassign(actor models.User, batchID, toRouteID uint, note string) (*models.RouteAssignment, error) {
	if actor.Role != models.RoleAdmin {
		return nil, ErrForbidden
	}
	var na *models.RouteAssignment
	err := r.s.db.Transaction(func(tx *gorm.DB) error {
		var b models.Batch
		if err := tx.First(&b, batchID).Error; err != nil {
			return ErrNotFound
		}
		old, err := r.activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if old.Status != models.AssignInTransit && old.Status != models.AssignAccepted &&
			old.Status != models.AssignAssigned && old.Status != models.AssignFailed {
			return fmt.Errorf("%w: 任务已完成，不可改派", ErrConflict)
		}
		var route models.Route
		if err := tx.First(&route, toRouteID).Error; err != nil {
			return fmt.Errorf("%w: 目标路线不存在", ErrValidation)
		}
		if toRouteID == old.RouteID {
			return fmt.Errorf("%w: 新旧路线相同，无需改派", ErrValidation)
		}
		now := clockNow()
		// 先关闭旧任务（释放唯一 active）
		if err := tx.Model(old).Updates(map[string]any{
			"active": false, "status": models.AssignReassigned, "reassigned_at": now,
		}).Error; err != nil {
			return err
		}
		na = &models.RouteAssignment{
			BatchID: batchID, RouteID: toRouteID, CourierID: route.CourierID,
			Seq: old.Seq + 1, Status: models.AssignAssigned, Active: true, HandoverNote: note,
		}
		if err := tx.Create(na).Error; err != nil {
			return err
		}
		tx.Model(&models.Batch{}).Where("id = ?", batchID).
			Updates(map[string]any{"status": models.BatchAssigned})
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: na.ID,
			FromRouteID: &old.RouteID, ToRouteID: &toRouteID,
			ActorID: actor.ID, ActorName: actor.Name, Type: "reassign",
			Note: fmt.Sprintf("由路线#%d 改派至路线「%s」：%s", old.RouteID, route.Name, note),
		})
		r.s.addAudit(tx, batchID, actor.ID, actor.Name, "route_reassign",
			fmt.Sprintf("路线#%d → 路线#%d：%s", old.RouteID, toRouteID, note))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return na, nil
}

// Deliver 送达企业，等待前台签收
func (r *RouteService) Deliver(actor models.User, batchID uint, note string) error {
	if actor.Role != models.RoleCourier {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		a, err := r.activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if a.CourierID != actor.ID {
			return fmt.Errorf("%w: 非当前路线配送员", ErrForbidden)
		}
		if a.Status != models.AssignInTransit {
			return fmt.Errorf("%w: 任务状态 %s 不可送达", ErrConflict, a.Status)
		}
		now := clockNow()
		tx.Model(a).Updates(map[string]any{"status": models.AssignDelivered, "delivered_at": now})
		tx.Model(&models.Batch{}).Where("id = ?", batchID).
			Updates(map[string]any{"status": models.BatchDelivered, "delivered_at": now})
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: ptrU(a.RouteID),
			ActorID: actor.ID, ActorName: actor.Name, Type: "deliver", Note: note,
		})
		return nil
	})
}

// NoOne 无人签收：留下经过，任务标记失败但仍占用路线（等待改派或调度处理），餐盒计入在途
func (r *RouteService) NoOne(actor models.User, batchID uint, note string) error {
	if actor.Role != models.RoleCourier {
		return ErrForbidden
	}
	return r.s.db.Transaction(func(tx *gorm.DB) error {
		a, err := r.activeTx(tx, batchID)
		if err != nil {
			return err
		}
		if a.CourierID != actor.ID {
			return fmt.Errorf("%w: 非当前路线配送员", ErrForbidden)
		}
		if a.Status != models.AssignDelivered && a.Status != models.AssignInTransit {
			return fmt.Errorf("%w: 任务状态 %s 不可登记无人签收", ErrConflict, a.Status)
		}
		now := clockNow()
		tx.Model(a).Updates(map[string]any{"status": models.AssignFailed, "failed_at": now})
		tx.Model(&models.Batch{}).Where("id = ?", batchID).Update("status", models.BatchInTransit)
		r.s.addEvent(tx, models.RouteEvent{
			BatchID: batchID, AssignmentID: a.ID, ToRouteID: ptrU(a.RouteID),
			ActorID: actor.ID, ActorName: actor.Name, Type: "no_one", Note: note,
		})
		r.s.addAudit(tx, batchID, actor.ID, actor.Name, "no_one", note)
		// 失败回执留痕（非签收）
		tx.Create(&models.Receipt{
			BatchID: batchID, AssignmentID: a.ID, Status: "no_one", Note: note, CreatedAt: now,
		})
		return nil
	})
}

func (r *RouteService) activeTx(tx *gorm.DB, batchID uint) (*models.RouteAssignment, error) {
	var a models.RouteAssignment
	err := tx.Where("batch_id = ? AND active = ?", batchID, true).First(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoActiveRoute
		}
		return nil, err
	}
	return &a, nil
}

func ptrU(id uint) *uint { return &id }
