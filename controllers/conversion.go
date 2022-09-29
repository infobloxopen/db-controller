package controllers

import (
	"sort"

	crossplanedb "github.com/crossplane/provider-aws/apis/database/v1beta1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

type RDSInstanceTags []crossplanedb.Tag

type DBClaimTags []persistancev1.Tag

func (r DBClaimTags) RDSInstanceTags() RDSInstanceTags {
	sort.Sort(r)
	tags := make(RDSInstanceTags, 0, len(r))
	for _, t := range r {
		tags = append(tags, crossplanedb.Tag{Key: t.Key, Value: t.Value})
	}
	return tags
}

// implementation of sort interface to allow canolicalization of tags
func (r DBClaimTags) Len() int {
	return len(r)
}

func (r DBClaimTags) Less(i, j int) bool {
	return r[i].Key < r[j].Key
}

func (r DBClaimTags) Swap(i, j int) { r[i], r[j] = r[j], r[i] }

func SortTags(input []persistancev1.Tag) {
	sort.Sort(DBClaimTags(input))
}
