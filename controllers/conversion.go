package controllers

import (
	"sort"

	crossplanerds "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

type DBTags []*crossplanerds.Tag

type DBClaimTags []persistancev1.Tag

func (r DBClaimTags) DBTags() DBTags {
	sort.Sort(r)
	tags := make(DBTags, 0, len(r))
	for _, t := range r {
		key := t.Key
		val := t.Value
		tag := crossplanerds.Tag{Key: &key, Value: &val}
		tags = append(tags, &tag)
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
