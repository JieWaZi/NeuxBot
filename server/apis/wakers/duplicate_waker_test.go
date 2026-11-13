package wakers

import (
	"path/filepath"
	"testing"

	"github.com/JieWaZi/NeuxBot/server/constants"
	"github.com/JieWaZi/NeuxBot/server/global"
	"github.com/JieWaZi/NeuxBot/server/internal/profile"
	"github.com/JieWaZi/NeuxBot/server/repositories/controllers"
	"github.com/JieWaZi/NeuxBot/server/repositories/models"
	"github.com/gin-gonic/gin"
	"github.com/shrewx/ginx/pkg/conf"
	"github.com/shrewx/ginx/pkg/dbhelper"
)

func TestDuplicateWakerCopiesProfileAndReplays(t *testing.T) {
	database, err := dbhelper.NewDB(conf.DB{Type: conf.Sqlite, DBName: filepath.Join(t.TempDir(), "wakers.db"), Migrate: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := database.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	oldWakers, oldProfiles, oldTransactions := global.WakerControllerManager, global.ProfileDocumentControllerManager, global.TransactionControllerManager
	wakers := controllers.NewWakerController(database.DB)
	profiles := controllers.NewProfileDocumentController(database.DB)
	global.WakerControllerManager, global.ProfileDocumentControllerManager = wakers, profiles
	global.TransactionControllerManager = controllers.NewTransactionController(dbhelper.NewGormTransactionManager(database.DB))
	t.Cleanup(func() {
		global.WakerControllerManager, global.ProfileDocumentControllerManager, global.TransactionControllerManager = oldWakers, oldProfiles, oldTransactions
	})

	source := models.Waker{
		ID: "source", Name: "Reviewer", RoleName: "Editor", Bio: "Review drafts", Avatar: "avatar",
		OnboardingStatus: constants.WakerOnboardingStatusReady, OnboardingChatID: "original-chat",
		Expertise: []profile.Expertise{{Title: "Review"}},
		WorkStyle: []string{"Concise"}, WorkMethods: []profile.WorkMethod{{Name: "Review", Steps: []string{"Check evidence"}}},
		DefaultModel: &models.WakerDefaultModel{RuntimeID: "codex", AccountID: "system", Model: "sample"},
	}
	ctx, _ := gin.CreateTestContext(nil)
	if err := wakers.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	const content = "# Working agreement\n\nReview drafts."
	if err := profiles.SaveWakerAgents(ctx, source.ID, content, 1); err != nil {
		t.Fatal(err)
	}
	op := &DuplicateWaker{WakerID: source.ID, Body: DuplicateWakerRequest{RequestID: "copy-one"}}
	first, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := first.(DuplicateWakerResponse).WakerID
	replay, err := op.Output(ctx)
	if err != nil || replay != first {
		t.Fatalf("copy replay = %v, %v", replay, err)
	}
	copy, err := wakers.Get(ctx, id)
	if err != nil || copy.ID == source.ID || copy.Name != "Reviewer (2)" ||
		copy.OnboardingStatus != constants.WakerOnboardingStatusReady || copy.OnboardingChatID != "" ||
		copy.RoleName != source.RoleName || copy.Bio != source.Bio || copy.Avatar != source.Avatar ||
		copy.DefaultModel == nil || *copy.DefaultModel != *source.DefaultModel ||
		len(copy.Expertise) != 1 || len(copy.WorkStyle) != 1 || len(copy.WorkMethods) != 1 {
		t.Fatalf("copied partner = %+v, %v", copy, err)
	}
	doc, err := profiles.GetWakerDocument(ctx, id, constants.ProfileDocumentKindAgents)
	if err != nil || doc.Content != content {
		t.Fatalf("copied profile = %+v, %v", doc, err)
	}
	op.Body.RequestID = "copy-two"
	second, err := op.Output(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copy, err = wakers.Get(ctx, second.(DuplicateWakerResponse).WakerID)
	if err != nil || copy.Name != "Reviewer (3)" {
		t.Fatalf("second copied name = %q, %v", copy.Name, err)
	}
}
