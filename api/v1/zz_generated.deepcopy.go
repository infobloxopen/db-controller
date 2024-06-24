//go:build !ignore_autogenerated

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
	if in.SecretCreatedAt != nil {
		in, out := &in.SecretCreatedAt, &out.SecretCreatedAt
		*out = (*in).DeepCopy()
	}
	if in.SecretUpdatedAt != nil {
		in, out := &in.SecretUpdatedAt, &out.SecretUpdatedAt
		*out = (*in).DeepCopy()
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
func (in *SchemaStatus) DeepCopyInto(out *SchemaStatus) {
	*out = *in
	out.UsersStatus = in.UsersStatus
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaStatus.
func (in *SchemaStatus) DeepCopy() *SchemaStatus {
	if in == nil {
		return nil
	}
	out := new(SchemaStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaUserClaim) DeepCopyInto(out *SchemaUserClaim) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	out.Status = in.Status
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaUserClaim.
func (in *SchemaUserClaim) DeepCopy() *SchemaUserClaim {
	if in == nil {
		return nil
	}
	out := new(SchemaUserClaim)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SchemaUserClaim) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaUserClaimList) DeepCopyInto(out *SchemaUserClaimList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SchemaUserClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaUserClaimList.
func (in *SchemaUserClaimList) DeepCopy() *SchemaUserClaimList {
	if in == nil {
		return nil
	}
	out := new(SchemaUserClaimList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SchemaUserClaimList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaUserClaimSpec) DeepCopyInto(out *SchemaUserClaimSpec) {
	*out = *in
	if in.Class != nil {
		in, out := &in.Class, &out.Class
		*out = new(string)
		**out = **in
	}
	if in.Schemas != nil {
		in, out := &in.Schemas, &out.Schemas
		*out = make([]SchemaUserType, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaUserClaimSpec.
func (in *SchemaUserClaimSpec) DeepCopy() *SchemaUserClaimSpec {
	if in == nil {
		return nil
	}
	out := new(SchemaUserClaimSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaUserClaimStatus) DeepCopyInto(out *SchemaUserClaimStatus) {
	*out = *in
	out.Schemas = in.Schemas
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaUserClaimStatus.
func (in *SchemaUserClaimStatus) DeepCopy() *SchemaUserClaimStatus {
	if in == nil {
		return nil
	}
	out := new(SchemaUserClaimStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaUserType) DeepCopyInto(out *SchemaUserType) {
	*out = *in
	if in.Users != nil {
		in, out := &in.Users, &out.Users
		*out = make([]UserType, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaUserType.
func (in *SchemaUserType) DeepCopy() *SchemaUserType {
	if in == nil {
		return nil
	}
	out := new(SchemaUserType)
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

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UserStatusType) DeepCopyInto(out *UserStatusType) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UserStatusType.
func (in *UserStatusType) DeepCopy() *UserStatusType {
	if in == nil {
		return nil
	}
	out := new(UserStatusType)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UserType) DeepCopyInto(out *UserType) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UserType.
func (in *UserType) DeepCopy() *UserType {
	if in == nil {
		return nil
	}
	out := new(UserType)
	in.DeepCopyInto(out)
	return out
}
