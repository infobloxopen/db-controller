package controllers

import (
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type BaseReconciler struct {
	client.Client
	Class    string
	Config   *viper.Viper
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}
