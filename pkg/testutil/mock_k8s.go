package testutil

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/testing"
)

// MockK8sClient provides a mock Kubernetes client for testing
type MockK8sClient struct {
	*fake.Clientset
	pods      []corev1.Pod
	watchChan chan watch.Event
}

// NewMockK8sClient creates a new mock Kubernetes client
func NewMockK8sClient() *MockK8sClient {
	client := &MockK8sClient{
		Clientset: fake.NewSimpleClientset(),
		pods:      []corev1.Pod{},
		watchChan: make(chan watch.Event, 100),
	}
	return client
}

// AddPod adds a pod to the mock client
func (m *MockK8sClient) AddPod(pod *corev1.Pod) {
	m.pods = append(m.pods, *pod)
	m.Clientset.Tracker().Add(pod)
}

// UpdatePod updates a pod in the mock client
func (m *MockK8sClient) UpdatePod(pod *corev1.Pod) {
	m.Clientset.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, pod.Namespace)
	
	// Send watch event
	m.watchChan <- watch.Event{
		Type:   watch.Modified,
		Object: pod,
	}
}

// SendWatchEvent sends a watch event
func (m *MockK8sClient) SendWatchEvent(eventType watch.EventType, obj runtime.Object) {
	m.watchChan <- watch.Event{
		Type:   eventType,
		Object: obj,
	}
}

// GetWatchChannel returns the watch event channel
func (m *MockK8sClient) GetWatchChannel() <-chan watch.Event {
	return m.watchChan
}

// SetupWatchReactor sets up a watch reactor for the mock client
func (m *MockK8sClient) SetupWatchReactor(resource string) {
	m.Clientset.PrependWatchReactor(resource, func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		return true, &MockWatcher{events: m.watchChan}, nil
	})
}

// MockWatcher implements watch.Interface for testing
type MockWatcher struct {
	events <-chan watch.Event
}

func (w *MockWatcher) Stop() {}

func (w *MockWatcher) ResultChan() <-chan watch.Event {
	return w.events
}

// CreateMilvusHelmPod creates a Milvus pod with Helm labels
func CreateMilvusHelmPod(name, namespace, instanceName string, restartCount int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":     "milvus",
				"helm.sh/chart":              "milvus",
				"app.kubernetes.io/instance": instanceName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "milvus",
					Image: "milvusdb/milvus:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "milvus",
					RestartCount: restartCount,
					Ready:        true,
				},
			},
		},
	}
}

// CreateMilvusOperatorPod creates a Milvus pod with Operator labels
func CreateMilvusOperatorPod(name, namespace, instanceName string, restartCount int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "milvus-operator",
				"milvus.io/instance":           instanceName,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "milvus",
					Image: "milvusdb/milvus:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "milvus",
					RestartCount: restartCount,
					Ready:        true,
				},
			},
		},
	}
}

// CreatePodWithLabels creates a pod with custom labels
func CreatePodWithLabels(name, namespace string, labels map[string]string, restartCount int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "container",
					Image: "test:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "container",
					RestartCount: restartCount,
					Ready:        true,
				},
			},
		},
	}
}