package sqlite

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"rttys/internal/domain/vncendpoint"
)

type VncEndpointRepo struct{ db *gorm.DB }

func NewVncEndpointRepo(db *gorm.DB) *VncEndpointRepo { return &VncEndpointRepo{db: db} }

type vncEndpointRow struct {
	ID          int64  `gorm:"column:id;primaryKey"`
	Name        string `gorm:"column:name"`
	Addr        string `gorm:"column:addr"`
	ViaDevice   string `gorm:"column:via_device"`
	Description string `gorm:"column:description"`
	PasswordEnc string `gorm:"column:password_enc"`
	CreatedAt   int64  `gorm:"column:created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at"`
}

func (vncEndpointRow) TableName() string { return "vnc_endpoints" }

func (r vncEndpointRow) toDomain() *vncendpoint.Endpoint {
	return &vncendpoint.Endpoint{
		ID:          r.ID,
		Name:        r.Name,
		Addr:        r.Addr,
		ViaDevice:   r.ViaDevice,
		Description: r.Description,
		PasswordEnc: r.PasswordEnc,
		HasPassword: r.PasswordEnc != "",
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func (r *VncEndpointRepo) List(ctx context.Context) ([]*vncendpoint.Endpoint, error) {
	var rows []vncEndpointRow
	if err := r.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*vncendpoint.Endpoint, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toDomain())
	}
	return out, nil
}

func (r *VncEndpointRepo) FindByID(ctx context.Context, id int64) (*vncendpoint.Endpoint, error) {
	var row vncEndpointRow
	err := r.db.WithContext(ctx).First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.toDomain(), nil
}

func (r *VncEndpointRepo) Create(ctx context.Context, e *vncendpoint.Endpoint) (int64, error) {
	now := time.Now().Unix()
	row := vncEndpointRow{
		Name:        e.Name,
		Addr:        e.Addr,
		ViaDevice:   e.ViaDevice,
		Description: e.Description,
		PasswordEnc: e.PasswordEnc,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return 0, err
	}
	return row.ID, nil
}

func (r *VncEndpointRepo) Update(ctx context.Context, e *vncendpoint.Endpoint) error {
	return r.db.WithContext(ctx).Model(&vncEndpointRow{}).
		Where("id = ?", e.ID).
		Updates(map[string]any{
			"name":         e.Name,
			"addr":         e.Addr,
			"via_device":   e.ViaDevice,
			"description":  e.Description,
			"password_enc": e.PasswordEnc,
			"updated_at":   time.Now().Unix(),
		}).Error
}

func (r *VncEndpointRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&vncEndpointRow{}, id).Error
}
