//go:build !ignore_autogenerated

/*
Copyright 2024.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if in.Class != nil {
		in, out := &in.Class, &out.Class
		*out = new(string)
		**out = **in
	}
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
	if in.EnableReplicationRole != nil {
		in, out := &in.EnableReplicationRole, &out.EnableReplicationRole
		*out = new(bool)
		**out = **in
	}
	if in.EnableSuperUser != nil {
		in, out := &in.EnableSuperUser, &out.EnableSuperUser
		*out = new(bool)
		**out = **in
	}
	if in.Tags != nil {
		in, out := &in.Tags, &out.Tags
		*out = make([]Tag, len(*in))
		copy(*out, *in)
	}
	if in.PreferredMaintenanceWindow != nil {
		in, out := &in.PreferredMaintenanceWindow, &out.PreferredMaintenanceWindow
		*out = new(string)
		**out = **in
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
	in.NewDB.DeepCopyInto(&out.NewDB)
	in.ActiveDB.DeepCopyInto(&out.ActiveDB)
	in.OldDB.DeepCopyInto(&out.OldDB)
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
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
func (in *DbRoleClaim) DeepCopyInto(out *DbRoleClaim) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DbRoleClaim.
func (in *DbRoleClaim) DeepCopy() *DbRoleClaim {
	if in == nil {
		return nil
	}
	out := new(DbRoleClaim)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DbRoleClaim) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DbRoleClaimList) DeepCopyInto(out *DbRoleClaimList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DbRoleClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DbRoleClaimList.
func (in *DbRoleClaimList) DeepCopy() *DbRoleClaimList {
	if in == nil {
		return nil
	}
	out := new(DbRoleClaimList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DbRoleClaimList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DbRoleClaimSpec) DeepCopyInto(out *DbRoleClaimSpec) {
	*out = *in
	if in.Class != nil {
		in, out := &in.Class, &out.Class
		*out = new(string)
		**out = **in
	}
	if in.SourceDatabaseClaim != nil {
		in, out := &in.SourceDatabaseClaim, &out.SourceDatabaseClaim
		*out = new(SourceDatabaseClaim)
		**out = **in
	}
	if in.SchemaRoleMap != nil {
		in, out := &in.SchemaRoleMap, &out.SchemaRoleMap
		*out = make(map[string]RoleType, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DbRoleClaimSpec.
func (in *DbRoleClaimSpec) DeepCopy() *DbRoleClaimSpec {
	if in == nil {
		return nil
	}
	out := new(DbRoleClaimSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DbRoleClaimStatus) DeepCopyInto(out *DbRoleClaimStatus) {
	*out = *in
	if in.SecretUpdatedAt != nil {
		in, out := &in.SecretUpdatedAt, &out.SecretUpdatedAt
		*out = (*in).DeepCopy()
	}
	in.SchemaRoleStatus.DeepCopyInto(&out.SchemaRoleStatus)
	if in.SchemasRolesUpdatedAt != nil {
		in, out := &in.SchemasRolesUpdatedAt, &out.SchemasRolesUpdatedAt
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DbRoleClaimStatus.
func (in *DbRoleClaimStatus) DeepCopy() *DbRoleClaimStatus {
	if in == nil {
		return nil
	}
	out := new(DbRoleClaimStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaRoleStatus) DeepCopyInto(out *SchemaRoleStatus) {
	*out = *in
	if in.SchemaStatus != nil {
		in, out := &in.SchemaStatus, &out.SchemaStatus
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.RoleStatus != nil {
		in, out := &in.RoleStatus, &out.RoleStatus
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaRoleStatus.
func (in *SchemaRoleStatus) DeepCopy() *SchemaRoleStatus {
	if in == nil {
		return nil
	}
	out := new(SchemaRoleStatus)
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
func (in *SourceDatabaseClaim) DeepCopyInto(out *SourceDatabaseClaim) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SourceDatabaseClaim.
func (in *SourceDatabaseClaim) DeepCopy() *SourceDatabaseClaim {
	if in == nil {
		return nil
	}
	out := new(SourceDatabaseClaim)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Status) DeepCopyInto(out *Status) {
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
	if in.SourceDataFrom != nil {
		in, out := &in.SourceDataFrom, &out.SourceDataFrom
		*out = new(SourceDataFrom)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Status.
func (in *Status) DeepCopy() *Status {
	if in == nil {
		return nil
	}
	out := new(Status)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *StatusForOldDB) DeepCopyInto(out *StatusForOldDB) {
	*out = *in
	if in.ConnectionInfo != nil {
		in, out := &in.ConnectionInfo, &out.ConnectionInfo
		*out = new(DatabaseClaimConnectionInfo)
		**out = **in
	}
	if in.PostMigrationActionStartedAt != nil {
		in, out := &in.PostMigrationActionStartedAt, &out.PostMigrationActionStartedAt
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new StatusForOldDB.
func (in *StatusForOldDB) DeepCopy() *StatusForOldDB {
	if in == nil {
		return nil
	}
	out := new(StatusForOldDB)
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
