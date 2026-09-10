package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrOIDCIdentityConflict = errors.New("OIDC identity is already bound to another user")

func (r *userRepository) GetOIDCIdentity(ctx context.Context, issuer, subject string) (*types.UserOIDCIdentity, error) {
	var identity types.UserOIDCIdentity
	err := r.db.WithContext(ctx).Where("issuer = ? AND subject = ?", issuer, subject).First(&identity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &identity, err
}

func (r *userRepository) BindOIDCIdentity(ctx context.Context, identity *types.UserOIDCIdentity) error {
	if identity == nil || identity.Issuer == "" || identity.Subject == "" || identity.UserID == "" {
		return errors.New("OIDC identity requires issuer, subject and user ID")
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "issuer"}, {Name: "subject"}}, DoNothing: true,
	}).Create(identity).Error; err != nil {
		return err
	}
	stored, err := r.GetOIDCIdentity(ctx, identity.Issuer, identity.Subject)
	if err != nil {
		return err
	}
	if stored == nil || stored.UserID != identity.UserID {
		return ErrOIDCIdentityConflict
	}
	return nil
}
