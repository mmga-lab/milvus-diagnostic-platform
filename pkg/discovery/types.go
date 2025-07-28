package discovery

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MilvusInstance struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace"`
	Type        DeploymentType    `json:"type"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Status      InstanceStatus    `json:"status"`
	CreatedAt   metav1.Time       `json:"createdAt"`
	Pods        []PodInfo         `json:"pods"`
}

type DeploymentType string

const (
	DeploymentTypeHelm     DeploymentType = "helm"
	DeploymentTypeOperator DeploymentType = "operator"
)

type InstanceStatus string

const (
	InstanceStatusRunning    InstanceStatus = "running"
	InstanceStatusFailed     InstanceStatus = "failed"
	InstanceStatusPending    InstanceStatus = "pending"
	InstanceStatusTerminating InstanceStatus = "terminating"
)

type PodInfo struct {
	Name            string    `json:"name"`
	Namespace       string    `json:"namespace"`
	Status          string    `json:"status"`
	RestartCount    int32     `json:"restartCount"`
	LastRestart     metav1.Time `json:"lastRestart"`
	ContainerStatuses []ContainerStatusInfo `json:"containerStatuses"`
}

type ContainerStatusInfo struct {
	Name         string `json:"name"`
	RestartCount int32  `json:"restartCount"`
	Ready        bool   `json:"ready"`
	LastTerminationReason string `json:"lastTerminationReason,omitempty"`
	LastTerminationMessage string `json:"lastTerminationMessage,omitempty"`
}

type RestartEvent struct {
	PodName       string    `json:"podName"`
	PodNamespace  string    `json:"podNamespace"`
	ContainerName string    `json:"containerName"`
	RestartTime   metav1.Time `json:"restartTime"`
	Reason        string    `json:"reason"`
	Message       string    `json:"message"`
	ExitCode      int32     `json:"exitCode"`
	Signal        int32     `json:"signal"`
	InstanceName  string    `json:"instanceName"`
	IsPanic       bool      `json:"isPanic"`
}