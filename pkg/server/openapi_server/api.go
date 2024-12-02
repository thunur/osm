// SPDX-License-Identifier: MIT

package openapi_server

import (
	"context"
	"net/http"
)

// DefaultApiRouter defines the required methods for binding the api requests to a responses for the DefaultApi
// The DefaultApiRouter implementation should parse necessary information from the http request,
// pass the data to a DefaultApiServicer to perform the required actions, then write the service results to the http response.
type DefaultApiRouter interface {
	ComputeRoute(http.ResponseWriter, *http.Request)
	GetNodes(http.ResponseWriter, *http.Request)
	GetSearchSpace(http.ResponseWriter, *http.Request)
	SetNavigator(http.ResponseWriter, *http.Request)
	FindShipRoute(http.ResponseWriter, *http.Request)
	FindRoadRoute(http.ResponseWriter, *http.Request)
	GetTrafficInfo(http.ResponseWriter, *http.Request)
}

// DefaultApiServicer defines the api actions for the DefaultApi service
// This interface intended to stay up to date with the openapi yaml used to generate it,
// while the service implementation can ignored with the .openapi-generator-ignore file
// and updated with the logic required for the API.
type DefaultApiServicer interface {
	ComputeRoute(context.Context, RouteRequest) (ImplResponse, error)
	GetNodes(context.Context) (ImplResponse, error)
	GetSearchSpace(context.Context) (ImplResponse, error)
	SetNavigator(context.Context, NavigatorRequest) (ImplResponse, error)
	FindShipRoute(context.Context, ShipRouteRequest) (ImplResponse, error)
	FindRoadRoute(context.Context, RoadRouteRequest) (ImplResponse, error)
	GetTrafficInfo(context.Context, TrafficInfoRequest) (ImplResponse, error)
}

// NavigatorConfig defines the configuration for the navigator
type NavigatorConfig struct {
	UseSpeedProfile bool
	ConsiderTraffic bool
	AvoidTolls      bool
	VehicleType     string
	PreferHighway   bool
}

// 添加新的请求结构体
type ShipRouteRequest struct {
	Origin      Point `json:"origin"`
	Destination Point `json:"destination"`
}

type RoadRouteRequest struct {
	Origin      Point `json:"origin"`
	Destination Point `json:"destination"`
	// 道路导航特有参数
	VehicleType   string `json:"vehicleType,omitempty"`
	AvoidTolls    bool   `json:"avoidTolls,omitempty"`
	PreferHighway bool   `json:"preferHighway,omitempty"`
}

type TrafficInfoRequest struct {
	Location Point   `json:"location"`
	Radius   float32 `json:"radius,omitempty"`
}
