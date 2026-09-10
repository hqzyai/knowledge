package types

import "time"

// UserOIDCIdentity binds a provider's immutable subject to a local account.
// Email is deliberately not part of the key: providers may change it.
type UserOIDCIdentity struct {
	Issuer    string    `gorm:"primaryKey;size:512"`
	Subject   string    `gorm:"primaryKey;size:255"`
	UserID    string    `gorm:"size:36;not null;index"`
	CreatedAt time.Time `gorm:"not null"`
}

func (UserOIDCIdentity) TableName() string { return "user_oidc_identities" }
