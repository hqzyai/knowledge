package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOIDCIdentityBindingIsUniqueAndCannotBeReassigned(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&types.UserOIDCIdentity{}); err != nil {
		t.Fatal(err)
	}
	r := NewUserRepository(db)
	ctx := context.Background()
	identity := &types.UserOIDCIdentity{Issuer: "https://idp.example", Subject: "same-subject", UserID: "original"}
	if err = r.BindOIDCIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if err = r.BindOIDCIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	other := *identity
	other.UserID = "other"
	if err = r.BindOIDCIdentity(ctx, &other); !errors.Is(err, ErrOIDCIdentityConflict) {
		t.Fatalf("reassignment was accepted: %v", err)
	}
	bound, err := r.GetOIDCIdentity(ctx, identity.Issuer, identity.Subject)
	if err != nil || bound.UserID != "original" {
		t.Fatalf("original binding changed: %v %v", bound, err)
	}
	other.Issuer = "https://other.example"
	if err = r.BindOIDCIdentity(ctx, &other); err != nil {
		t.Fatal(err)
	}
}
