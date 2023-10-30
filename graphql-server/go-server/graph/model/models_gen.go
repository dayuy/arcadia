// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package model

import (
	"time"
)

type Condition struct {
	Type               string     `json:"type"`
	Status             string     `json:"status"`
	LastTransitionTime time.Time  `json:"lastTransitionTime"`
	LastSuccessfulTime *time.Time `json:"lastSuccessfulTime,omitempty"`
	Reason             string     `json:"reason"`
	Message            *string    `json:"message,omitempty"`
}

type CreateDatasource struct {
	Name       string  `json:"name"`
	Namespace  string  `json:"namespace"`
	URL        *string `json:"url,omitempty"`
	Authsecret *string `json:"authsecret,omitempty"`
	Insecure   *bool   `json:"insecure,omitempty"`
}

type Datasource struct {
	Kind              string                 `json:"kind"`
	APIVersion        string                 `json:"apiVersion"`
	Name              string                 `json:"name"`
	Namespace         string                 `json:"namespace"`
	UID               string                 `json:"uid"`
	ResourceVersion   string                 `json:"resourceVersion"`
	Generation        int                    `json:"generation"`
	CreationTimestamp time.Time              `json:"creationTimestamp"`
	DeletionTimestamp *time.Time             `json:"deletionTimestamp,omitempty"`
	Labels            map[string]interface{} `json:"labels,omitempty"`
	Annotations       map[string]interface{} `json:"annotations,omitempty"`
	Finalizers        []string               `json:"finalizers,omitempty"`
	Spec              *DatasourceSpec        `json:"spec,omitempty"`
	Status            *DatasourceStatus      `json:"status,omitempty"`
}

type DatasourceSpec struct {
	URL        *string `json:"url,omitempty"`
	Authsecret *string `json:"authsecret,omitempty"`
}

type DatasourceStatus struct {
	Conditions []*Condition `json:"conditions,omitempty"`
}

type QueryDatasource struct {
	Name          *string `json:"name,omitempty"`
	Namespace     string  `json:"namespace"`
	LabelSelector *string `json:"labelSelector,omitempty"`
	FieldSelector *string `json:"fieldSelector,omitempty"`
}