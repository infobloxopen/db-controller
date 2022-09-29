//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by controller-gen. DO NOT EDIT.

package v1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Database) DeepCopyInto(out *Database) {
	*out = *in
	if in.SecretRef != nil {
		in, out := &in.SecretRef, &out.SecretRef
		*out = new(SecretRef)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Database.
func (in *Database) DeepCopy() *Database {
	if in == nil {
		return nil
	}
	out := new(Database)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseClaim) DeepCopyInto(out *DatabaseClaim) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseClaim.
func (in *DatabaseClaim) DeepCopy() *DatabaseClaim {
	if in == nil {
		return nil
	}
	out := new(DatabaseClaim)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DatabaseClaim) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseClaimConnectionInfo) DeepCopyInto(out *DatabaseClaimConnectionInfo) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseClaimConnectionInfo.
func (in *DatabaseClaimConnectionInfo) DeepCopy() *DatabaseClaimConnectionInfo {
	if in == nil {
		return nil
	}
	out := new(DatabaseClaimConnectionInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseClaimList) DeepCopyInto(out *DatabaseClaimList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DatabaseClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseClaimList.
func (in *DatabaseClaimList) DeepCopy() *DatabaseClaimList {
	if in == nil {
		return nil
	}
	out := new(DatabaseClaimList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DatabaseClaimList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseClaimSpec) DeepCopyInto(out *DatabaseClaimSpec) {
	*out = *in
	if in.SourceDataFrom != nil {
		in, out := &in.SourceDataFrom, &out.SourceDataFrom
		*out = new(SourceDataFrom)
		(*in).DeepCopyInto(*out)
	}
	if in.UseExistingSource != nil {
		in, out := &in.UseExistingSource, &out.UseExistingSource
		*out = new(bool)
		**out = **in
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]Tag, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseClaimSpec.
func (in *DatabaseClaimSpec) DeepCopy() *DatabaseClaimSpec {
	if in == nil {
		return nil
	}
	out := new(DatabaseClaimSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DatabaseClaimStatus) DeepCopyInto(out *DatabaseClaimStatus) {
	*out = *in
	if in.DbCreatedAt != nil {
		in, out := &in.DbCreatedAt, &out.DbCreatedAt
		*out = (*in).DeepCopy()
	}
	if in.ConnectionInfo != nil {
		in, out := &in.ConnectionInfo, &out.ConnectionInfo
		*out = new(DatabaseClaimConnectionInfo)
		**out = **in
	}
	if in.ConnectionInfoUpdatedAt != nil {
		in, out := &in.ConnectionInfoUpdatedAt, &out.ConnectionInfoUpdatedAt
		*out = (*in).DeepCopy()
	}
	if in.UserUpdatedAt != nil {
		in, out := &in.UserUpdatedAt, &out.UserUpdatedAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DatabaseClaimStatus.
func (in *DatabaseClaimStatus) DeepCopy() *DatabaseClaimStatus {
	if in == nil {
		return nil
	}
	out := new(DatabaseClaimStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *S3BackupConfiguration) DeepCopyInto(out *S3BackupConfiguration) {
	*out = *in
	if in.Prefix != nil {
		in, out := &in.Prefix, &out.Prefix
		*out = new(string)
		**out = **in
	}
	if in.SourceEngine != nil {
		in, out := &in.SourceEngine, &out.SourceEngine
		*out = new(SQLEngine)
		**out = **in
	}
	if in.SourceEngineVersion != nil {
		in, out := &in.SourceEngineVersion, &out.SourceEngineVersion
		*out = new(string)
		**out = **in
	}
	if in.SecretRef != nil {
		in, out := &in.SecretRef, &out.SecretRef
		*out = new(SecretRef)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new S3BackupConfiguration.
func (in *S3BackupConfiguration) DeepCopy() *S3BackupConfiguration {
	if in == nil {
		return nil
	}
	out := new(S3BackupConfiguration)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SecretRef) DeepCopyInto(out *SecretRef) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SecretRef.
func (in *SecretRef) DeepCopy() *SecretRef {
	if in == nil {
		return nil
	}
	out := new(SecretRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SourceDataFrom) DeepCopyInto(out *SourceDataFrom) {
	*out = *in
	if in.Database != nil {
		in, out := &in.Database, &out.Database
		*out = new(Database)
		(*in).DeepCopyInto(*out)
	}
	if in.S3 != nil {
		in, out := &in.S3, &out.S3
		*out = new(S3BackupConfiguration)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SourceDataFrom.
func (in *SourceDataFrom) DeepCopy() *SourceDataFrom {
	if in == nil {
		return nil
	}
	out := new(SourceDataFrom)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Tag) DeepCopyInto(out *Tag) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Tag.
func (in *Tag) DeepCopy() *Tag {
	if in == nil {
		return nil
	}
	out := new(Tag)
	in.DeepCopyInto(out)
	return out
}
