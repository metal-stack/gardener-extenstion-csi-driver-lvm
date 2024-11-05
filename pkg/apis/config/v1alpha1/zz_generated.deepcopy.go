//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
2024 Copyright metal-stack Authors.
*/

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha1

import (
	configv1alpha1 "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ControllerConfiguration) DeepCopyInto(out *ControllerConfiguration) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	if in.DevicePattern != nil {
		in, out := &in.DevicePattern, &out.DevicePattern
		*out = new(string)
		**out = **in
	}
	if in.HostWritePath != nil {
		in, out := &in.HostWritePath, &out.HostWritePath
		*out = new(string)
		**out = **in
	}
	if in.HealthCheckConfig != nil {
		in, out := &in.HealthCheckConfig, &out.HealthCheckConfig
		*out = new(configv1alpha1.HealthCheckConfig)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControllerConfiguration.
func (in *ControllerConfiguration) DeepCopy() *ControllerConfiguration {
	if in == nil {
		return nil
	}
	out := new(ControllerConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ControllerConfiguration) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}