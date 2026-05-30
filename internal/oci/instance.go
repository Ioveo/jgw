// Package oci 实例相关 API 操作
package oci

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// InstanceConfig 要创建的实例配置
type InstanceConfig struct {
	CompartmentID      string
	AvailabilityDomain string
	DisplayName        string
	Shape              string
	OCPUs              float64
	MemoryGB           float64
	ImageID            string
	ImageFilter        string // 模糊匹配镜像的名称过滤器，如 "ubuntu 22.04"
	SubnetID           string
	AssignPublicIP     bool
	SSHPublicKey       string
	BootVolumeSizeGB   int    // 引导卷大小 (GB)，0 表示使用镜像默认值（通常 46.6GB）
	BootVolumeVPU      int    // 引导卷性能 (VPU/GB)：10=均衡, 20=高性能, 30~120=超高性能，0 表示默认(10)
}

// LaunchInstance 调用 OCI API 创建实例
// 成功返回 Instance，失败返回 APIError
func (c *Client) LaunchInstance(cfg InstanceConfig, ad string) (*Instance, *APIError) {
	assignPublicIP := cfg.AssignPublicIP
	req := LaunchInstanceRequest{
		AvailabilityDomain: ad,
		CompartmentID:      cfg.CompartmentID,
		DisplayName:        cfg.DisplayName,
		Shape:              cfg.Shape,
		SourceDetails: InstanceSourceViaImageDetails{
			SourceType:          "image",
			ImageID:             cfg.ImageID,
			BootVolumeSizeInGBs: cfg.BootVolumeSizeGB,
			BootVolumeVpusPerGB: cfg.BootVolumeVPU,
		},
		CreateVnicDetails: CreateVnicDetails{
			SubnetID:       cfg.SubnetID,
			AssignPublicIp: &assignPublicIP,
		},
		Metadata: map[string]string{
			"ssh_authorized_keys": cfg.SSHPublicKey,
		},
	}

	// Flex 实例需要指定 ShapeConfig
	if cfg.OCPUs > 0 || cfg.MemoryGB > 0 {
		req.ShapeConfig = &ShapeConfig{
			OCPUs:       cfg.OCPUs,
			MemoryInGBs: cfg.MemoryGB,
		}
	}

	body, statusCode, err := c.doRequest(http.MethodPost, "/20160918/instances", req)
	if err != nil {
		return nil, &APIError{Code: "NetworkError", Message: err.Error(), Status: 0}
	}

	if statusCode == http.StatusOK || statusCode == http.StatusCreated {
		var inst Instance
		if err := json.Unmarshal(body, &inst); err != nil {
			return nil, &APIError{Code: "ParseError", Message: fmt.Sprintf("解析响应失败: %v", err)}
		}
		return &inst, nil
	}

	return nil, parseAPIError(body, statusCode)
}

// ListAvailabilityDomains 获取该区域所有可用域
// OCI Identity API 端点与 IAAS 不同，需单独构造请求
func (c *Client) ListAvailabilityDomains(compartmentID string) ([]AvailabilityDomainInfo, *APIError) {
	// Identity API 使用独立的 base URL
	identityBase := fmt.Sprintf("https://identity.%s.oraclecloud.com", c.cfg.Region)
	path := fmt.Sprintf("/20160918/availabilityDomains?compartmentId=%s", compartmentID)
	fullURL := identityBase + path

	body, statusCode, err := c.doRequestAbsolute(http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, &APIError{Code: "NetworkError", Message: err.Error()}
	}

	if statusCode != http.StatusOK {
		return nil, parseAPIError(body, statusCode)
	}

	var ads []AvailabilityDomainInfo
	if err := json.Unmarshal(body, &ads); err != nil {
		return nil, &APIError{Code: "ParseError", Message: fmt.Sprintf("解析 AD 列表失败: %v", err)}
	}
	return ads, nil
}

// ListImages 获取符合 Shape 的镜像列表
func (c *Client) ListImages(compartmentID, shape string) ([]ImageInfo, *APIError) {
	path := fmt.Sprintf("/20160918/images?compartmentId=%s&shape=%s", compartmentID, shape)
	body, statusCode, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, &APIError{Code: "NetworkError", Message: err.Error()}
	}

	if statusCode != http.StatusOK {
		return nil, parseAPIError(body, statusCode)
	}

	var images []ImageInfo
	if err := json.Unmarshal(body, &images); err != nil {
		return nil, &APIError{Code: "ParseError", Message: fmt.Sprintf("解析镜像列表失败: %v", err)}
	}
	return images, nil
}

// ListVcns 获取虚拟云网络（VCN）列表
func (c *Client) ListVcns(compartmentID string) ([]VcnInfo, *APIError) {
	path := fmt.Sprintf("/20160918/vcns?compartmentId=%s", compartmentID)
	body, statusCode, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, &APIError{Code: "NetworkError", Message: err.Error()}
	}

	if statusCode != http.StatusOK {
		return nil, parseAPIError(body, statusCode)
	}

	var vcns []VcnInfo
	if err := json.Unmarshal(body, &vcns); err != nil {
		return nil, &APIError{Code: "ParseError", Message: fmt.Sprintf("解析 VCN 列表失败: %v", err)}
	}
	return vcns, nil
}

// ListSubnets 获取指定 VCN 下的子网列表
func (c *Client) ListSubnets(compartmentID, vcnID string) ([]SubnetInfo, *APIError) {
	path := fmt.Sprintf("/20160918/subnets?compartmentId=%s&vcnId=%s", compartmentID, vcnID)
	body, statusCode, err := c.doRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, &APIError{Code: "NetworkError", Message: err.Error()}
	}

	if statusCode != http.StatusOK {
		return nil, parseAPIError(body, statusCode)
	}

	var subnets []SubnetInfo
	if err := json.Unmarshal(body, &subnets); err != nil {
		return nil, &APIError{Code: "ParseError", Message: fmt.Sprintf("解析子网列表失败: %v", err)}
	}
	return subnets, nil
}

