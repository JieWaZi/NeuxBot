package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants/status_error"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/shrewx/ginx/pkg/statuserror"
)

func TestProjectSourceControllerContract(t *testing.T) {
	db := newTestDB(t)
	controller := NewProjectSourceController(db)
	ctx := context.Background()
	source := models.ProjectSource{
		ID:        "11111111111111111111111111111111",
		Name:      "desktop",
		Path:      "/tmp/ally-desktop",
		CreatedAt: 1,
		UpdatedAt: 1,
	}
	if err := controller.Ensure(ctx, source); err != nil {
		t.Fatalf("ensure project source: %v", err)
	}
	source.Name = "desktop-updated"
	source.UpdatedAt = 2
	if err := controller.Ensure(ctx, source); err != nil {
		t.Fatalf("refresh project source: %v", err)
	}
	stored, err := controller.Get(ctx, source.ID)
	if err != nil || stored.Name != source.Name || stored.UpdatedAt != source.UpdatedAt {
		t.Fatalf("stored project source = %+v, error = %v", stored, err)
	}

	if _, err := controller.Get(ctx, "missing"); err != nil {
		var commonErr statuserror.CommonError
		if !errors.As(err, &commonErr) ||
			commonErr.Code() != status_error.ProjectSourceUnavailable.Code() {
			t.Fatalf("missing project source error = %v", err)
		}
	} else {
		t.Fatal("missing project source lookup succeeded")
	}
}
