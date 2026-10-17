package services

import (
	"strings"
	"testing"

	"github.com/Grenco/omarchy-blueprint/internal/model"
)

// Every descriptor this provider emits is complete and names its own
// category, so Auto Sync never has to guess (capability audit §4).
func TestEffectDescriptorsAreComplete(t *testing.T) {
	for _, e := range []*model.Effect{commandEffect("enable"), commandEffect("disable"), commandEffect("mask"), commandEffect("start"), commandEffect("daemon-reload"), artifactEffect("u.service", model.Operation{File: &model.FileWrite{ExpectedMissing: true}}), artifactEffect("u.service.d/x.conf", model.Operation{File: &model.FileWrite{}}), artifactEffect("u.service", model.Operation{Delete: &model.FileDelete{}})} {
		if !e.Complete() || !strings.HasPrefix(e.Family, "services.") {
			t.Fatalf("incomplete descriptor %+v", *e)
		}
	}
}
