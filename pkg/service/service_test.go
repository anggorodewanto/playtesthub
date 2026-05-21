package service

import (
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// TestServiceDescriptorMethods guards against accidental RPC removal
// during future proto edits. If the M1 + M2 + M3 surface in PRD §4.7
// shrinks silently, this test fails. M2/M3 RPCs are declared on the
// service in their phase-1 (docs/STATUS.md) so codegen + admin/CLI
// work can land before handlers do; the embedded
// UnimplementedPlaytesthubServiceServer makes runtime calls return
// gRPC Unimplemented until the gating phase implements each handler.
func TestServiceDescriptorMethods(t *testing.T) {
	want := map[string]bool{
		// M1
		"GetPublicPlaytest":        true,
		"GetPublicConfig":          true,
		"GetPlaytestForPlayer":     true,
		"WhoAmI":                   true,
		"AdminGetPlaytest":         true,
		"ListPlaytests":            true,
		"CreatePlaytest":           true,
		"EditPlaytest":             true,
		"SoftDeletePlaytest":       true,
		"TransitionPlaytestStatus": true,
		"Signup":                   true,
		"GetApplicantStatus":       true,
		"ExchangeDiscordCode":      true,
		// M2
		"AcceptNDA":        true,
		"GetGrantedCode":   true,
		"ListApplicants":   true,
		"ApproveApplicant": true,
		"RejectApplicant":  true,
		"RetryDM":          true,
		"UploadCodes":      true,
		"TopUpCodes":       true,
		"SyncFromAGS":      true,
		"GetCodePool":      true,
		// M3
		"CreateSurvey":         true,
		"EditSurvey":           true,
		"GetSurvey":            true,
		"SubmitSurveyResponse": true,
		"ListSurveyResponses":  true,
		"ListAuditLog":         true,
		"RetryFailedDms":       true,
		// M4
		"GetWorkerHealth": true,
		// M5.B (ADT linkage + player download)
		"ListADTLinkages":         true,
		"StartADTLink":            true,
		"CompleteADTLink":         true,
		"UnlinkADT":               true,
		"RecoverADTLinkage":       true,
		"ListADTBuilds":           true,
		"ListADTGames":            true,
		"GetADTClientDiagnostics": true,
		"GetADTDownloadInfo":      true,
		// M5.C (participants + announcements)
		"GetPlaytestParticipants": true,
		"CreateAnnouncement":      true,
		"ListAnnouncements":       true,
	}

	methods := pb.PlaytesthubService_ServiceDesc.Methods
	if len(methods) != len(want) {
		t.Fatalf("method count mismatch: got %d, want %d", len(methods), len(want))
	}

	for _, m := range methods {
		if !want[m.MethodName] {
			t.Errorf("unexpected method in service descriptor: %q", m.MethodName)
		}
		delete(want, m.MethodName)
	}
	for missing := range want {
		t.Errorf("missing method in service descriptor: %q", missing)
	}
}
