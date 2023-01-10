package pgctl

import "errors"

type StateEnum int

const (
	S_Initial StateEnum = iota
	S_ValidateConnection
	S_CreatePublication
	S_CopySchema
	S_CreateSubscription
	S_EnableSubscription
	S_CutOverReadinessCheck
	S_ResetTargetSequence
	S_RerouteTargetSecret
	S_WaitToDisableSource
	S_DisableSourceAccess
	S_ValidateMigrationStatus
	S_DisableSubscription
	S_DeleteSubscription
	S_DeletePublication
	S_Completed
	S_Retry
)

var stateMap = make(map[string]StateEnum)

func (s StateEnum) String() string {
	switch s {
	case S_Initial:
		return "initial"
	case S_ValidateConnection:
		return "validate_connection"
	case S_CreatePublication:
		return "create_publication"
	case S_CopySchema:
		return "copy_schema"
	case S_CreateSubscription:
		return "create_subscription"
	case S_EnableSubscription:
		return "enable_subscription"
	case S_CutOverReadinessCheck:
		return "cut_over_readiness_check"
	case S_ResetTargetSequence:
		return "reset_target_sequence"
	case S_RerouteTargetSecret:
		return "reroute_target_secret"
	case S_WaitToDisableSource:
		return "wait_to_disable_source"
	case S_DisableSourceAccess:
		return "disable_source_access"
	case S_ValidateMigrationStatus:
		return "validate_migration_status"
	case S_DisableSubscription:
		return "disable_subscription"
	case S_DeleteSubscription:
		return "delete_subscription"
	case S_DeletePublication:
		return "delete_publication"
	case S_Completed:
		return "completed"
	case S_Retry:
		return "retry"

	}

	return "unknown"
}

func init() {
	stateMap[""] = S_Initial
	stateMap[S_Initial.String()] = S_Initial
	stateMap[S_ValidateConnection.String()] = S_ValidateConnection
	stateMap[S_CreatePublication.String()] = S_CreatePublication
	stateMap[S_CopySchema.String()] = S_CopySchema
	stateMap[S_CreateSubscription.String()] = S_CreateSubscription
	stateMap[S_EnableSubscription.String()] = S_EnableSubscription
	stateMap[S_CutOverReadinessCheck.String()] = S_CutOverReadinessCheck
	stateMap[S_ResetTargetSequence.String()] = S_ResetTargetSequence
	stateMap[S_RerouteTargetSecret.String()] = S_RerouteTargetSecret
	stateMap[S_DisableSourceAccess.String()] = S_DisableSourceAccess
	stateMap[S_ValidateMigrationStatus.String()] = S_ValidateMigrationStatus
	stateMap[S_WaitToDisableSource.String()] = S_WaitToDisableSource
	stateMap[S_DisableSubscription.String()] = S_DisableSubscription
	stateMap[S_DeleteSubscription.String()] = S_DeleteSubscription
	stateMap[S_DeletePublication.String()] = S_DeletePublication
	stateMap[S_Completed.String()] = S_Completed
	stateMap[S_Retry.String()] = S_Retry
}

func GetStateEnum(name string) (StateEnum, error) {
	if s, present := stateMap[name]; present {
		return s, nil
	} else {
		return -1, errors.New("unknown state")
	}
}
