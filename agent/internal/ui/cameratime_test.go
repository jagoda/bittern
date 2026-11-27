package ui

import (
	"net/url"
	"testing"
)

type fakeCameraTimeNotifier struct {
	ids []string
}

func (f *fakeCameraTimeNotifier) RequestSync(ids ...string) {
	f.ids = append(f.ids, ids...)
}

func TestWizardRequestsCameraTimeAfterApplyWithoutFailingAdoption(t *testing.T) {
	notifier := &fakeCameraTimeNotifier{}
	server, client := newTestServer(t, Options{
		Applier:    &fakeApplier{},
		CameraTime: notifier,
	})
	completeToReview(t, client, server.URL)
	statusCode, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != 200 {
		t.Fatalf("apply status: %d\n%s", statusCode, body)
	}
	assertContains(t, body, "Configuration applied")
	if len(notifier.ids) != 1 || notifier.ids[0] == "" {
		t.Fatalf("camera time was not requested for the adopted camera: %+v", notifier.ids)
	}
}
