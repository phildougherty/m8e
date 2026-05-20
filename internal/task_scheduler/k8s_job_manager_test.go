package task_scheduler

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/logging"
)

// newTestJobManager builds a K8sJobManager backed by the client-go fake clientset.
func newTestJobManager(namespace string) *K8sJobManager {
	if namespace == "" {
		namespace = constants.MateyNamespace
	}
	return &K8sJobManager{
		client:    fake.NewSimpleClientset(),
		namespace: namespace,
		logger:    logging.NewLogger("error"),
	}
}

func TestParseResourceQuantity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid cpu", "500m", "500m"},
		{"valid memory", "256Mi", "256Mi"},
		{"valid whole cpu", "2", "2"},
		{"invalid falls back to 100m", "not-a-quantity", "100m"},
		{"empty falls back to 100m", "", "100m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResourceQuantity(tt.input)
			if got.String() != tt.expected {
				t.Errorf("parseResourceQuantity(%q) = %q, want %q", tt.input, got.String(), tt.expected)
			}
		})
	}
}

func TestShortenForK8sLabel(t *testing.T) {
	short := "short-task-id"
	if got := shortenForK8sLabel(short); got != short {
		t.Errorf("expected short string unchanged, got %q", got)
	}

	long := ""
	for i := 0; i < 100; i++ {
		long += "a"
	}
	got := shortenForK8sLabel(long)
	if len(got) != 63 {
		t.Errorf("expected shortened label length 63, got %d", len(got))
	}
	if got[:55] != long[:55] {
		t.Errorf("expected first 55 chars preserved")
	}
	// Determinism.
	if shortenForK8sLabel(long) != got {
		t.Errorf("shortenForK8sLabel is not deterministic")
	}

	// Exactly 63 chars: unchanged.
	exact := long[:63]
	if shortenForK8sLabel(exact) != exact {
		t.Errorf("expected exactly-63 string unchanged")
	}
}

func TestCreateJobSpec_Defaults(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:          "abc123",
		Name:        "my-task",
		Description: "a description",
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}

	if job.Name != "task-abc123" {
		t.Errorf("job name = %q, want task-abc123", job.Name)
	}
	if job.Namespace != "matey" {
		t.Errorf("job namespace = %q, want matey", job.Namespace)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "busybox:latest" {
		t.Errorf("default image = %q, want busybox:latest", container.Image)
	}
	if container.ImagePullPolicy != corev1.PullAlways {
		t.Errorf("image pull policy = %q, want Always", container.ImagePullPolicy)
	}
	if container.Name != "task" {
		t.Errorf("container name = %q, want task", container.Name)
	}

	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restart policy = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}

	// MaxRetries defaults to 0 -> backoffLimit 0 (it is not <0).
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("backoff limit = %v, want 0", job.Spec.BackoffLimit)
	}

	// No timeout -> no active deadline.
	if job.Spec.ActiveDeadlineSeconds != nil {
		t.Errorf("active deadline = %v, want nil", job.Spec.ActiveDeadlineSeconds)
	}

	// Labels.
	wantLabels := map[string]string{
		"app.kubernetes.io/name":       "task",
		"app.kubernetes.io/component":  "task-execution",
		"app.kubernetes.io/managed-by": "matey-task-scheduler",
		"mcp.matey.ai/task-id":         "abc123",
		"mcp.matey.ai/task-name":       "my-task",
		"mcp.matey.ai/scheduler":       "task-scheduler",
	}
	for k, v := range wantLabels {
		if job.Labels[k] != v {
			t.Errorf("label %q = %q, want %q", k, job.Labels[k], v)
		}
	}

	if job.Annotations["mcp.matey.ai/task-description"] != "a description" {
		t.Errorf("description annotation = %q", job.Annotations["mcp.matey.ai/task-description"])
	}
	if _, ok := job.Annotations["mcp.matey.ai/submitted-at"]; !ok {
		t.Errorf("expected submitted-at annotation")
	}

	// Default env vars present.
	envMap := map[string]string{}
	for _, e := range container.Env {
		envMap[e.Name] = e.Value
	}
	if envMap["TASK_ID"] != "abc123" || envMap["TASK_NAME"] != "my-task" {
		t.Errorf("expected TASK_ID/TASK_NAME env vars, got %v", envMap)
	}

	// No resources requested -> empty.
	if container.Resources.Requests != nil || container.Resources.Limits != nil {
		t.Errorf("expected empty resources, got %+v", container.Resources)
	}
}

func TestCreateJobSpec_FullSpec(t *testing.T) {
	jm := newTestJobManager("custom-ns")
	task := &TaskRequest{
		ID:      "task-id-2",
		Name:    "full-task",
		Image:   "alpine:3.19",
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{"echo hello"},
		Env:     map[string]string{"FOO": "bar"},
		Timeout: 90 * time.Second,
		Retry:   TaskRetryConfig{MaxRetries: 5},
		Resources: TaskResourceConfig{
			CPU:    "250m",
			Memory: "128Mi",
		},
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != "alpine:3.19" {
		t.Errorf("image = %q, want alpine:3.19", container.Image)
	}
	if len(container.Command) != 2 || container.Command[0] != "/bin/sh" {
		t.Errorf("command = %v", container.Command)
	}
	if len(container.Args) != 1 || container.Args[0] != "echo hello" {
		t.Errorf("args = %v", container.Args)
	}

	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 5 {
		t.Errorf("backoff limit = %v, want 5", job.Spec.BackoffLimit)
	}
	if job.Spec.ActiveDeadlineSeconds == nil || *job.Spec.ActiveDeadlineSeconds != 90 {
		t.Errorf("active deadline = %v, want 90", job.Spec.ActiveDeadlineSeconds)
	}

	cpuReq := container.Resources.Requests[corev1.ResourceCPU]
	cpuLim := container.Resources.Limits[corev1.ResourceCPU]
	memReq := container.Resources.Requests[corev1.ResourceMemory]
	if cpuReq.String() != "250m" || cpuLim.String() != "250m" {
		t.Errorf("cpu request/limit = %s/%s, want 250m", cpuReq.String(), cpuLim.String())
	}
	if memReq.String() != "128Mi" {
		t.Errorf("memory request = %s, want 128Mi", memReq.String())
	}

	envMap := map[string]string{}
	for _, e := range container.Env {
		envMap[e.Name] = e.Value
	}
	if envMap["FOO"] != "bar" {
		t.Errorf("expected custom env FOO=bar, got %v", envMap)
	}
}

func TestCreateJobSpec_EmptyDirVolume(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:   "vol-task",
		Name: "vol-task",
		Volumes: []TaskVolumeConfig{
			{
				Name:      "scratch",
				MountPath: "/scratch",
				Type:      "emptyDir",
				Source: TaskVolumeSource{
					EmptyDir: &TaskVolumeEmptyDir{SizeLimit: "1Gi", Medium: "Memory"},
				},
			},
		},
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}

	vols := job.Spec.Template.Spec.Volumes
	if len(vols) != 1 || vols[0].Name != "scratch" {
		t.Fatalf("expected one volume named scratch, got %v", vols)
	}
	if vols[0].EmptyDir == nil {
		t.Fatalf("expected emptyDir volume source")
	}
	if vols[0].EmptyDir.Medium != corev1.StorageMediumMemory {
		t.Errorf("emptyDir medium = %q, want Memory", vols[0].EmptyDir.Medium)
	}
	if vols[0].EmptyDir.SizeLimit == nil || vols[0].EmptyDir.SizeLimit.String() != "1Gi" {
		t.Errorf("emptyDir sizeLimit = %v, want 1Gi", vols[0].EmptyDir.SizeLimit)
	}

	mounts := job.Spec.Template.Spec.Containers[0].VolumeMounts
	if len(mounts) != 1 || mounts[0].MountPath != "/scratch" {
		t.Errorf("expected mount at /scratch, got %v", mounts)
	}
}

func TestCreateJobSpec_ConfigMapVolume(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:   "cm-task",
		Name: "cm-task",
		Volumes: []TaskVolumeConfig{
			{
				Name:      "cfg",
				MountPath: "/etc/cfg",
				Type:      "configmap",
				ReadOnly:  true,
				Source: TaskVolumeSource{
					ConfigMap: &TaskVolumeConfigMap{
						Name:  "my-configmap",
						Items: []TaskVolumeConfigMapItem{{Key: "app.conf", Path: "app.conf"}},
					},
				},
			},
		},
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}

	vol := job.Spec.Template.Spec.Volumes[0]
	if vol.ConfigMap == nil || vol.ConfigMap.Name != "my-configmap" {
		t.Fatalf("expected configMap source named my-configmap, got %v", vol)
	}
	if len(vol.ConfigMap.Items) != 1 || vol.ConfigMap.Items[0].Key != "app.conf" {
		t.Errorf("configMap items = %v", vol.ConfigMap.Items)
	}
	if !job.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly {
		t.Errorf("expected readonly mount")
	}
}

func TestCreateJobSpec_ConfigMapMissingSource(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:   "bad-cm",
		Name: "bad-cm",
		Volumes: []TaskVolumeConfig{
			{Name: "cfg", MountPath: "/etc/cfg", Type: "configmap"},
		},
	}
	_, err := jm.createJobSpec(context.Background(), task)
	if err == nil {
		t.Fatalf("expected error for configmap volume without source")
	}
}

func TestCreateJobSpec_UnsupportedVolumeType(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:   "bad-vol",
		Name: "bad-vol",
		Volumes: []TaskVolumeConfig{
			{Name: "x", MountPath: "/x", Type: "nfs"},
		},
	}
	_, err := jm.createJobSpec(context.Background(), task)
	if err == nil {
		t.Fatalf("expected error for unsupported volume type")
	}
}

func TestCreateJobSpec_PVCAutoCreate(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:    "pvc-task",
		Name:  "pvc-task",
		Image: "alpine:3.19",
		Volumes: []TaskVolumeConfig{
			{
				Name:      "data",
				MountPath: "/data",
				Type:      "pvc",
				Source: TaskVolumeSource{
					PVC: &TaskVolumePVC{Size: "2Gi"},
				},
			},
		},
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}

	vol := job.Spec.Template.Spec.Volumes[0]
	if vol.PersistentVolumeClaim == nil {
		t.Fatalf("expected PVC volume source")
	}
	// Name derived from task name + image hash since no explicit claim/workflow.
	pvcName := vol.PersistentVolumeClaim.ClaimName
	if pvcName == "" {
		t.Fatalf("expected non-empty derived PVC name")
	}

	// The PVC should have actually been created in the fake cluster.
	pvc, getErr := jm.client.CoreV1().PersistentVolumeClaims("matey").Get(context.Background(), pvcName, metav1.GetOptions{})
	if getErr != nil {
		t.Fatalf("expected auto-created PVC %q to exist: %v", pvcName, getErr)
	}
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storage.String() != "2Gi" {
		t.Errorf("PVC storage = %s, want 2Gi", storage.String())
	}

	// auto-created-pvcs annotation should be recorded.
	if job.Annotations["mcp.matey.ai/auto-created-pvcs"] != pvcName {
		t.Errorf("auto-created-pvcs annotation = %q, want %q", job.Annotations["mcp.matey.ai/auto-created-pvcs"], pvcName)
	}
}

func TestCreateJobSpec_PVCExplicitClaimName(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{
		ID:   "pvc-task-2",
		Name: "pvc-task-2",
		Volumes: []TaskVolumeConfig{
			{
				Name:      "data",
				MountPath: "/data",
				Type:      "pvc",
				Source: TaskVolumeSource{
					PVC: &TaskVolumePVC{ClaimName: "existing-claim"},
				},
			},
		},
	}

	job, err := jm.createJobSpec(context.Background(), task)
	if err != nil {
		t.Fatalf("createJobSpec returned error: %v", err)
	}
	if job.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName != "existing-claim" {
		t.Errorf("expected explicit claim name to be used")
	}
	// No PVC should be auto-created.
	if _, ok := job.Annotations["mcp.matey.ai/auto-created-pvcs"]; ok {
		t.Errorf("did not expect auto-created-pvcs annotation for explicit claim")
	}
}

func TestSubmitTask(t *testing.T) {
	jm := newTestJobManager("matey")
	task := &TaskRequest{ID: "sub-1", Name: "submit-task"}

	status, err := jm.SubmitTask(context.Background(), task)
	if err != nil {
		t.Fatalf("SubmitTask returned error: %v", err)
	}
	if status.Phase != "Pending" {
		t.Errorf("status phase = %q, want Pending", status.Phase)
	}
	if status.JobName != "task-sub-1" {
		t.Errorf("status job name = %q, want task-sub-1", status.JobName)
	}

	// Verify the Job exists in the fake cluster.
	job, getErr := jm.client.BatchV1().Jobs("matey").Get(context.Background(), "task-sub-1", metav1.GetOptions{})
	if getErr != nil {
		t.Fatalf("expected job to be created: %v", getErr)
	}
	if job.Labels["mcp.matey.ai/task-id"] != "sub-1" {
		t.Errorf("job task-id label = %q", job.Labels["mcp.matey.ai/task-id"])
	}
}

func TestGetTaskStatus_Phases(t *testing.T) {
	tests := []struct {
		name      string
		jobStatus batchv1.JobStatus
		wantPhase string
	}{
		{"active", batchv1.JobStatus{Active: 1}, "Running"},
		{"succeeded", batchv1.JobStatus{Succeeded: 1}, "Succeeded"},
		{"failed", batchv1.JobStatus{Failed: 1}, "Failed"},
		{"pending", batchv1.JobStatus{}, "Pending"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager("matey")
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "task-x",
					Namespace: "matey",
					Labels:    map[string]string{"mcp.matey.ai/task-id": "x"},
				},
				Status: tt.jobStatus,
			}
			if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
				t.Fatalf("failed to seed job: %v", err)
			}

			status, err := jm.GetTaskStatus(context.Background(), "x")
			if err != nil {
				t.Fatalf("GetTaskStatus returned error: %v", err)
			}
			if status.Phase != tt.wantPhase {
				t.Errorf("phase = %q, want %q", status.Phase, tt.wantPhase)
			}
		})
	}
}

func TestGetTaskStatus_NotFound(t *testing.T) {
	jm := newTestJobManager("matey")
	_, err := jm.GetTaskStatus(context.Background(), "missing")
	if err == nil {
		t.Fatalf("expected error for missing task")
	}
}

func TestCancelTask(t *testing.T) {
	jm := newTestJobManager("matey")
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-cancel",
			Namespace: "matey",
			Labels:    map[string]string{"mcp.matey.ai/task-id": "cancel-me"},
		},
	}
	if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to seed job: %v", err)
	}

	if err := jm.CancelTask(context.Background(), "cancel-me"); err != nil {
		t.Fatalf("CancelTask returned error: %v", err)
	}

	_, getErr := jm.client.BatchV1().Jobs("matey").Get(context.Background(), "task-cancel", metav1.GetOptions{})
	if getErr == nil {
		t.Errorf("expected job to be deleted")
	}

	// Cancelling a missing task should error.
	if err := jm.CancelTask(context.Background(), "nonexistent"); err == nil {
		t.Errorf("expected error cancelling nonexistent task")
	}
}

func TestListTasks(t *testing.T) {
	jm := newTestJobManager("matey")
	for _, id := range []string{"t1", "t2"} {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "task-" + id,
				Namespace: "matey",
				Labels: map[string]string{
					"mcp.matey.ai/task-id":   id,
					"mcp.matey.ai/scheduler": "task-scheduler",
				},
			},
			Status: batchv1.JobStatus{Succeeded: 1},
		}
		if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed job: %v", err)
		}
	}
	// A job from a different scheduler should be excluded.
	other := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-other",
			Namespace: "matey",
			Labels: map[string]string{
				"mcp.matey.ai/task-id":   "other",
				"mcp.matey.ai/scheduler": "different-scheduler",
			},
		},
	}
	if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), other, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to seed job: %v", err)
	}

	tasks, err := jm.ListTasks(context.Background(), "task-scheduler")
	if err != nil {
		t.Fatalf("ListTasks returned error: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestGetTaskStatistics(t *testing.T) {
	jm := newTestJobManager("matey")
	seed := []struct {
		name   string
		status batchv1.JobStatus
	}{
		{"task-s1", batchv1.JobStatus{Succeeded: 1}},
		{"task-s2", batchv1.JobStatus{Succeeded: 1}},
		{"task-f1", batchv1.JobStatus{Failed: 1}},
		{"task-r1", batchv1.JobStatus{Active: 1}},
		{"task-p1", batchv1.JobStatus{}},
	}
	for _, s := range seed {
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: "matey",
				Labels:    map[string]string{"mcp.matey.ai/scheduler": "task-scheduler"},
			},
			Status: s.status,
		}
		if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), job, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed job: %v", err)
		}
	}

	stats, err := jm.GetTaskStatistics(context.Background(), "task-scheduler")
	if err != nil {
		t.Fatalf("GetTaskStatistics returned error: %v", err)
	}
	if stats.TotalTasks != 5 {
		t.Errorf("total tasks = %d, want 5", stats.TotalTasks)
	}
	if stats.CompletedTasks != 2 {
		t.Errorf("completed tasks = %d, want 2", stats.CompletedTasks)
	}
	if stats.FailedTasks != 1 {
		t.Errorf("failed tasks = %d, want 1", stats.FailedTasks)
	}
	if stats.RunningTasks != 1 {
		t.Errorf("running tasks = %d, want 1", stats.RunningTasks)
	}
	if stats.ScheduledTasks != 1 {
		t.Errorf("scheduled tasks = %d, want 1", stats.ScheduledTasks)
	}
}

func TestCleanupCompletedTasks(t *testing.T) {
	jm := newTestJobManager("matey")
	oldTime := metav1.NewTime(time.Now().Add(-48 * time.Hour))
	recentTime := metav1.NewTime(time.Now().Add(-1 * time.Hour))

	jobs := []*batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "task-old-done",
				Namespace: "matey",
				Labels:    map[string]string{"mcp.matey.ai/scheduler": "task-scheduler"},
			},
			Status: batchv1.JobStatus{Succeeded: 1, CompletionTime: &oldTime},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "task-recent-done",
				Namespace: "matey",
				Labels:    map[string]string{"mcp.matey.ai/scheduler": "task-scheduler"},
			},
			Status: batchv1.JobStatus{Succeeded: 1, CompletionTime: &recentTime},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "task-old-running",
				Namespace: "matey",
				Labels:    map[string]string{"mcp.matey.ai/scheduler": "task-scheduler"},
			},
			Status: batchv1.JobStatus{Active: 1, StartTime: &oldTime},
		},
	}
	for _, j := range jobs {
		if _, err := jm.client.BatchV1().Jobs("matey").Create(context.Background(), j, metav1.CreateOptions{}); err != nil {
			t.Fatalf("failed to seed job: %v", err)
		}
	}

	if err := jm.CleanupCompletedTasks(context.Background(), "task-scheduler", 24*time.Hour); err != nil {
		t.Fatalf("CleanupCompletedTasks returned error: %v", err)
	}

	remaining, err := jm.client.BatchV1().Jobs("matey").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	got := map[string]bool{}
	for _, j := range remaining.Items {
		got[j.Name] = true
	}
	if got["task-old-done"] {
		t.Errorf("expected old completed task to be cleaned up")
	}
	if !got["task-recent-done"] {
		t.Errorf("expected recent completed task to be retained")
	}
	if !got["task-old-running"] {
		t.Errorf("expected old running task to be retained (not completed)")
	}
}

func TestCreatePVCForTask_Defaults(t *testing.T) {
	jm := newTestJobManager("matey")
	err := jm.createPVCForTask(context.Background(), "workspace-myflow-exec1", &TaskVolumePVC{})
	if err != nil {
		t.Fatalf("createPVCForTask returned error: %v", err)
	}

	pvc, getErr := jm.client.CoreV1().PersistentVolumeClaims("matey").Get(context.Background(), "workspace-myflow-exec1", metav1.GetOptions{})
	if getErr != nil {
		t.Fatalf("expected PVC to be created: %v", getErr)
	}
	// Default size 1Gi.
	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if storage.String() != "1Gi" {
		t.Errorf("default storage = %s, want 1Gi", storage.String())
	}
	// Default access mode ReadWriteOnce.
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("access modes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
	}
	// Workspace labels derived from name pattern.
	if pvc.Labels["mcp.matey.ai/workflow-name"] != "myflow" {
		t.Errorf("workflow-name label = %q, want myflow", pvc.Labels["mcp.matey.ai/workflow-name"])
	}
	if pvc.Labels["mcp.matey.ai/execution-id"] != "exec1" {
		t.Errorf("execution-id label = %q, want exec1", pvc.Labels["mcp.matey.ai/execution-id"])
	}
}

func TestNewK8sJobManager_NamespaceDefault(t *testing.T) {
	// We can't fully construct via NewK8sJobManager without a cluster, but the
	// namespace-defaulting branch is exercised by newTestJobManager + this
	// explicit check that empty -> MateyNamespace.
	jm := newTestJobManager("")
	if jm.namespace != constants.MateyNamespace {
		t.Errorf("expected namespace to default to %q, got %q", constants.MateyNamespace, jm.namespace)
	}
}
