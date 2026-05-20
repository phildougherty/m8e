package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	contextpkg "github.com/phildougherty/m8e/internal/context"
)

// newMentionProcessorForTest builds a real contextpkg.MentionProcessor wired
// to a fake kubernetes clientset preloaded with the given runtime.Objects.
// We use the real processor (not a mock) because the value of the test is in
// proving the new MateyMCPServer path actually reaches the same backend code
// the context-mention rebuild added.
func newMentionProcessorForTest(t *testing.T, objects ...runtime.Object) *contextpkg.MentionProcessor {
	t.Helper()

	dir := t.TempDir()
	fd, err := contextpkg.NewFileDiscovery(dir)
	if err != nil {
		t.Fatalf("NewFileDiscovery: %v", err)
	}
	cm := contextpkg.NewContextManager(contextpkg.ContextConfig{MaxTokens: 32768}, nil)
	mp := contextpkg.NewMentionProcessor(dir, fd, cm)

	cs := k8sfake.NewSimpleClientset(objects...)
	mp.SetKubernetesClients(cs, nil)
	mp.SetNamespace("matey")

	return mp
}

func newPodForMentionTest(name, namespace string, phase corev1.PodPhase, waitingReason string, ready bool) *corev1.Pod {
	cs := corev1.ContainerStatus{Name: "main", Ready: ready}
	if waitingReason != "" {
		cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waitingReason, Message: "container is waiting"}
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app": name}},
		Status: corev1.PodStatus{
			Phase:             phase,
			ContainerStatuses: []corev1.ContainerStatus{cs},
		},
	}
}

func TestMentionTools_NotInitialized(t *testing.T) {
	mt := newMentionTools(nil)
	res, err := mt.processMentions(context.Background(), map[string]interface{}{"text": "anything"})
	if err == nil {
		t.Fatalf("expected error when processor nil; got %+v", res)
	}
	if res == nil || !res.IsError {
		t.Errorf("expected IsError result, got %+v", res)
	}

	res, err = mt.expandMentions(context.Background(), map[string]interface{}{"text": "anything"})
	if err == nil {
		t.Fatalf("expected error when processor nil; got %+v", res)
	}
	if res == nil || !res.IsError {
		t.Errorf("expected IsError result, got %+v", res)
	}
}

func TestMentionTools_MissingTextArg(t *testing.T) {
	mp := newMentionProcessorForTest(t)
	mt := newMentionTools(mp)

	if _, err := mt.processMentions(context.Background(), map[string]interface{}{}); err == nil {
		t.Errorf("expected error when text missing for processMentions")
	}
	if _, err := mt.expandMentions(context.Background(), map[string]interface{}{}); err == nil {
		t.Errorf("expected error when text missing for expandMentions")
	}
}

// TestExpandMentions_HappyPath proves the @problems mention is resolved against
// a fake k8s clientset and inlined into the returned expanded text. This is
// the regression guard for the dead-code-at-call-graph-level bug: it exercises
// the same MentionProcessor backend the production MateyMCPServer wires in.
func TestExpandMentions_HappyPath(t *testing.T) {
	healthy := newPodForMentionTest("matey-proxy", "matey", corev1.PodRunning, "", true)
	crash := newPodForMentionTest("matey-memory", "matey", corev1.PodRunning, "CrashLoopBackOff", false)
	mp := newMentionProcessorForTest(t, healthy, crash)
	mt := newMentionTools(mp)

	res, err := mt.expandMentions(context.Background(), map[string]interface{}{
		"text": "Hey, check @problems for me.",
	})
	if err != nil {
		t.Fatalf("expandMentions error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success result, got %+v", res)
	}

	out := res.Content[0].Text
	if !strings.Contains(out, "matey/matey-memory") {
		t.Errorf("expected expanded text to mention the crashing pod; got %q", out)
	}
	if !strings.Contains(out, "CrashLoopBackOff") {
		t.Errorf("expected reason in expanded text; got %q", out)
	}
	if strings.Contains(out, "matey/matey-proxy") {
		t.Errorf("healthy pod must not appear; got %q", out)
	}
	if !strings.Contains(out, "Hey, check") || !strings.Contains(out, "for me.") {
		t.Errorf("expected original surrounding text to remain; got %q", out)
	}
}

// TestProcessMentions_HappyPath verifies the structured JSON payload contains
// the resolved mention content.
func TestProcessMentions_HappyPath(t *testing.T) {
	crash := newPodForMentionTest("matey-memory", "matey", corev1.PodRunning, "CrashLoopBackOff", false)
	mp := newMentionProcessorForTest(t, crash)
	mt := newMentionTools(mp)

	res, err := mt.processMentions(context.Background(), map[string]interface{}{
		"text": "Diagnose @problems please.",
	})
	if err != nil {
		t.Fatalf("processMentions error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected success result, got %+v", res)
	}

	var payload struct {
		Text     string                   `json:"text"`
		Count    int                      `json:"count"`
		Mentions []map[string]interface{} `json:"mentions"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].Text), &payload); err != nil {
		t.Fatalf("expected JSON payload, got %q: %v", res.Content[0].Text, err)
	}
	if payload.Count != 1 {
		t.Errorf("expected exactly one mention, got %d (payload=%+v)", payload.Count, payload)
	}
	if len(payload.Mentions) == 0 {
		t.Fatalf("expected at least one resolved mention")
	}
	gotContent, _ := payload.Mentions[0]["content"].(string)
	if !strings.Contains(gotContent, "CrashLoopBackOff") {
		t.Errorf("expected resolved content to include CrashLoopBackOff, got %q", gotContent)
	}
}
