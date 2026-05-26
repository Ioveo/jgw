// Package oci OCI API 数据模型定义
package oci

// LaunchInstanceRequest POST /20160918/instances 请求体
type LaunchInstanceRequest struct {
	AvailabilityDomain string                    `json:"availabilityDomain"`
	CompartmentID      string                    `json:"compartmentId"`
	DisplayName        string                    `json:"displayName"`
	Shape              string                    `json:"shape"`
	ShapeConfig        *ShapeConfig              `json:"shapeConfig,omitempty"`
	SourceDetails      InstanceSourceViaImageDetails `json:"sourceDetails"`
	CreateVnicDetails  CreateVnicDetails          `json:"createVnicDetails"`
	Metadata           map[string]string          `json:"metadata,omitempty"`
}

// ShapeConfig Flex 实例规格（A1.Flex 必填）
type ShapeConfig struct {
	OCPUs       float64 `json:"ocpus"`
	MemoryInGBs float64 `json:"memoryInGBs"`
}

// InstanceSourceViaImageDetails 使用镜像创建实例
type InstanceSourceViaImageDetails struct {
	SourceType string `json:"sourceType"` // "image"
	ImageID    string `json:"imageId"`
}

// CreateVnicDetails 网卡配置
type CreateVnicDetails struct {
	SubnetID       string `json:"subnetId"`
	AssignPublicIp *bool  `json:"assignPublicIp,omitempty"`
}

// Instance OCI 实例响应结构（精简版）
type Instance struct {
	ID                 string            `json:"id"`
	DisplayName        string            `json:"displayName"`
	LifecycleState     string            `json:"lifecycleState"`
	AvailabilityDomain string            `json:"availabilityDomain"`
	Shape              string            `json:"shape"`
	TimeCreated        string            `json:"timeCreated"`
	Region             string            `json:"region"`
	FreeformTags       map[string]string `json:"freeformTags"`
}

// APIError OCI API 错误响应
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *APIError) Error() string {
	return e.Message
}

// IsOutOfCapacity 是否容量不足错误
func (e *APIError) IsOutOfCapacity() bool {
	return e.Code == "InternalError" || e.Code == "LimitExceeded" || e.Code == "OutOfHostCapacity" ||
		e.Message == "Out of host capacity."
}

// IsThrottled 是否触发限流
func (e *APIError) IsThrottled() bool {
	return e.Code == "TooManyRequests" || e.Status == 429
}

// IsAuthError 是否鉴权失败
func (e *APIError) IsAuthError() bool {
	return e.Code == "NotAuthenticated" || e.Code == "Unauthorized" ||
		e.Status == 401 || e.Status == 403
}

// AvailabilityDomainInfo AD 信息
type AvailabilityDomainInfo struct {
	Name          string `json:"name"`
	CompartmentID string `json:"compartmentId"`
}

// ImageInfo 镜像信息
type ImageInfo struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	OperatingSystem string `json:"operatingSystem"`
	OSVersion      string `json:"operatingSystemVersion"`
	LifecycleState string `json:"lifecycleState"`
}

// VcnInfo 虚拟云网络信息
type VcnInfo struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	LifecycleState string `json:"lifecycleState"`
}

// SubnetInfo 子网信息
type SubnetInfo struct {
	ID             string `json:"id"`
	DisplayName    string `json:"displayName"`
	LifecycleState string `json:"lifecycleState"`
	VcnID          string `json:"vcnId"`
}

