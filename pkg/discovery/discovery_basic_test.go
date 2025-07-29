package discovery

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"milvus-coredump-agent/pkg/config"
)

func TestBasicMilvusInstanceIdentification(t *testing.T) {
	config := &config.DiscoveryConfig{
		HelmReleaseLabels: []string{
			"app.kubernetes.io/name",
			"helm.sh/chart",
		},
		OperatorLabels: []string{
			"app.kubernetes.io/managed-by",
			"milvus.io/instance",
		},
	}

	discovery := &Discovery{config: config}

	tests := []struct {
		name               string
		pod                *corev1.Pod
		expectedIsInstance bool
		expectedType       DeploymentType
		description        string
	}{
		{
			name: "helm_milvus_pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "milvus-standalone",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":     "milvus",
						"helm.sh/chart":              "milvus",
						"app.kubernetes.io/instance": "test-release",
					},
				},
			},
			expectedIsInstance: true,
			expectedType:       DeploymentTypeHelm,
			description:        "Should identify Helm-managed Milvus instance",
		},
		{
			name: "operator_milvus_pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "milvus-cluster-proxy-0",
					Namespace: "milvus-system",
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "milvus-operator",
						"milvus.io/instance":           "test-cluster",
					},
				},
			},
			expectedIsInstance: true,
			expectedType:       DeploymentTypeOperator,
			description:        "Should identify Operator-managed Milvus instance",
		},
		{
			name: "non_milvus_pod",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app": "nginx",
					},
				},
			},
			expectedIsInstance: false,
			description:        "Should not identify non-Milvus pods as instances",
		},
		{
			name: "partial_helm_labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "partial-pod",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "milvus",
						// Missing helm.sh/chart label
					},
				},
			},
			expectedIsInstance: false,
			description:        "Should not identify pods with partial Helm labels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, isInstance := identifyBasicMilvusInstance(discovery, tt.pod)

			if isInstance != tt.expectedIsInstance {
				t.Errorf("%s: expected isInstance=%v, got %v", tt.description, tt.expectedIsInstance, isInstance)
				return
			}

			if !tt.expectedIsInstance {
				return
			}

			if instance == nil {
				t.Errorf("%s: expected instance but got nil", tt.description)
				return
			}

			if instance.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, instance.Type)
			}

			if instance.Name == "" {
				t.Error("instance name should not be empty")
			}

			if instance.Namespace != tt.pod.Namespace {
				t.Errorf("expected namespace %s, got %s", tt.pod.Namespace, instance.Namespace)
			}
		})
	}
}

// Helper function for basic instance identification
func identifyBasicMilvusInstance(d *Discovery, pod *corev1.Pod) (*MilvusInstance, bool) {
	labels := pod.Labels
	if labels == nil {
		return nil, false
	}

	// Check for Helm-managed instance
	helmMatch := true
	for _, labelKey := range d.config.HelmReleaseLabels {
		if _, exists := labels[labelKey]; !exists {
			helmMatch = false
			break
		}
	}

	if helmMatch && labels["app.kubernetes.io/name"] == "milvus" {
		instanceName := labels["app.kubernetes.io/instance"]
		if instanceName == "" {
			instanceName = pod.Name
		}

		return &MilvusInstance{
			Name:      instanceName,
			Namespace: pod.Namespace,
			Type:      DeploymentTypeHelm,
			Labels:    labels,
			CreatedAt: metav1.Now(),
		}, true
	}

	// Check for Operator-managed instance
	operatorMatch := true
	for _, labelKey := range d.config.OperatorLabels {
		if _, exists := labels[labelKey]; !exists {
			operatorMatch = false
			break
		}
	}

	if operatorMatch && labels["app.kubernetes.io/managed-by"] == "milvus-operator" {
		instanceName := labels["milvus.io/instance"]
		if instanceName == "" {
			instanceName = pod.Name
		}

		return &MilvusInstance{
			Name:      instanceName,
			Namespace: pod.Namespace,
			Type:      DeploymentTypeOperator,
			Labels:    labels,
			CreatedAt: metav1.Now(),
		}, true
	}

	return nil, false
}

func TestBasicPodRestartDetection(t *testing.T) {
	tests := []struct {
		name            string
		oldRestartCount int32
		newRestartCount int32
		expectedRestart bool
		description     string
	}{
		{
			name:            "normal_restart_increment",
			oldRestartCount: 1,
			newRestartCount: 2,
			expectedRestart: true,
			description:     "Should detect normal restart count increment",
		},
		{
			name:            "no_restart",
			oldRestartCount: 1,
			newRestartCount: 1,
			expectedRestart: false,
			description:     "Should not detect restart when count is same",
		},
		{
			name:            "restart_count_decrease",
			oldRestartCount: 2,
			newRestartCount: 1,
			expectedRestart: false,
			description:     "Should not detect restart when count decreases",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restart := detectBasicPodRestart(tt.oldRestartCount, tt.newRestartCount)

			if restart != tt.expectedRestart {
				t.Errorf("%s: expected restart=%v, got %v", tt.description, tt.expectedRestart, restart)
			}
		})
	}
}

// Helper function for basic restart detection
func detectBasicPodRestart(oldRestartCount, newRestartCount int32) bool {
	return newRestartCount > oldRestartCount
}