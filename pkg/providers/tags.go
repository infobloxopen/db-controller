package providers

const (
	BackupPolicyKey = "Backup"
)

var (
	OperationalTAGActive   = ProviderTag{Key: "operational-status", Value: "active"}
	OperationalTAGInactive = ProviderTag{Key: "operational-status", Value: "inactive"}
)

// ProviderTag represents a key-value tag format.
type ProviderTag struct {
	Key   string
	Value string
}

// ConvertToProviderTags converts any slice of struct tags to ProviderTag format.
func ConvertToProviderTags[T any](tags []T, extractFunc func(T) (string, string)) []ProviderTag {
	providerTags := make([]ProviderTag, 0, len(tags))
	for _, tag := range tags {
		key, value := extractFunc(tag)
		providerTags = append(providerTags, ProviderTag{Key: key, Value: value})
	}
	return providerTags
}

// ConvertFromProviderTags converts a slice of ProviderTag to another tag type.
func ConvertFromProviderTags[T any](providerTags []ProviderTag, transformFunc func(ProviderTag) T) []T {
	convertedTags := make([]T, 0, len(providerTags))
	for _, tag := range providerTags {
		convertedTags = append(convertedTags, transformFunc(tag))
	}
	return convertedTags
}

// ConvertMapToProviderTags converts a map[string]string to a slice of ProviderTag.
func ConvertMapToProviderTags(tags map[string]string) []ProviderTag {
	providerTags := make([]ProviderTag, 0, len(tags))
	for k, v := range tags {
		providerTags = append(providerTags, ProviderTag{Key: k, Value: v})
	}
	return providerTags
}

// MergeTags merges two slices of ProviderTag, ensuring unique keys (newTags override existingTags).
func MergeTags(existingTags, newTags []ProviderTag) []ProviderTag {
	tagMap := make(map[string]string)

	for _, tag := range existingTags {
		tagMap[tag.Key] = tag.Value
	}
	for _, tag := range newTags {
		tagMap[tag.Key] = tag.Value
	}

	return ConvertMapToProviderTags(tagMap)
}
