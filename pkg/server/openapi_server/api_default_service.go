package openapi_server

import (
	"context"
	"net/http"
	"sync"

	"github.com/natevvv/osm-ship-routing/pkg/geometry"
	"github.com/natevvv/osm-ship-routing/pkg/graph"
	"github.com/natevvv/osm-ship-routing/pkg/graph/path"
	"github.com/natevvv/osm-ship-routing/pkg/routing"
)

// DefaultApiService is a service that implements the logic for the DefaultApiServicer
// This service should implement the business logic for every endpoint for the DefaultApi API.
// Include any external packages or services that will be required by this service.
type DefaultApiService struct {
	router *routing.Router
	config NavigatorConfig
}

// NewDefaultApiService creates a default api service
func NewDefaultApiService(graphFile, contractedGraphFile, shortcutFile, nodeOrderingFile string, navigator *string, config NavigatorConfig) DefaultApiServicer {
	var g graph.Graph
	var contractedGraph graph.Graph
	var shortcuts []path.Shortcut
	var nodeOrdering [][]int

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		g = graph.NewAdjacencyArrayFromFmiFile(graphFile)
		wg.Done()
	}()
	go func() {
		contractedGraph = graph.NewAdjacencyArrayFromFmiFile(contractedGraphFile)
		wg.Done()
	}()
	go func() {
		shortcuts = path.ReadShortcutFile(shortcutFile)
		wg.Done()
	}()
	go func() {
		nodeOrdering = path.ReadNodeOrderingFile(nodeOrderingFile)
		wg.Done()
	}()
	wg.Wait()

	router := routing.NewRouter(g, contractedGraph, shortcuts, nodeOrdering, navigator)

	return &DefaultApiService{
		router: router,
		config: config,
	}
}

// ComputeRoute - Compute a new route
func (s *DefaultApiService) ComputeRoute(ctx context.Context, routeRequest RouteRequest) (ImplResponse, error) {
	origin := geometry.NewPoint(float64(routeRequest.Origin.Lat), float64(routeRequest.Origin.Lon))
	destination := geometry.NewPoint(float64(routeRequest.Destination.Lat), float64(routeRequest.Destination.Lon))

	routeConfig := routing.RouteConfig{
		VehicleType:     s.config.VehicleType,
		AvoidTolls:      s.config.AvoidTolls,
		PreferHighway:   s.config.PreferHighway,
		ConsiderTraffic: s.config.ConsiderTraffic,
		MaxSpeed:        getMaxSpeedForVehicle(s.config.VehicleType),
	}

	route := s.router.ComputeRoute(*origin, *destination, routeConfig)

	routeResult := RouteResult{Origin: routeRequest.Origin, Destination: routeRequest.Destination}
	if route.Exists {
		routeResult.Reachable = true
		waypoints := make([]Point, 0)
		for _, waypoint := range route.Waypoints {
			p := Point{Lat: float32(waypoint.Lat()), Lon: float32(waypoint.Lon())}
			waypoints = append(waypoints, p)
		}
		routeResult.Path = Path{Length: int32(route.Length), Waypoints: waypoints}
	} else {
		routeResult.Reachable = false
	}

	return Response(http.StatusOK, routeResult), nil
}

func (s *DefaultApiService) GetNodes(ctx context.Context) (ImplResponse, error) {
	points := s.router.GetNodes()

	vertices := make([]Point, 0)
	for _, point := range points {
		p := Point{Lat: float32(point.Lat()), Lon: float32(point.Lon())}
		vertices = append(vertices, p)
	}
	nodes := Nodes{Waypoints: vertices}

	return Response(http.StatusOK, nodes), nil
}

func (s *DefaultApiService) GetSearchSpace(ctx context.Context) (ImplResponse, error) {
	points := s.router.GetSearchSpace()

	vertices := make([]Point, 0)
	for _, point := range points {
		p := Point{Lat: float32(point.Lat()), Lon: float32(point.Lon())}
		vertices = append(vertices, p)
	}
	nodes := Nodes{Waypoints: vertices}

	return Response(http.StatusOK, nodes), nil
}

func (s *DefaultApiService) SetNavigator(ctx context.Context, navigatorRequest NavigatorRequest) (ImplResponse, error) {
	success := s.router.SetNavigator(navigatorRequest.Navigator)

	if !success {
		return Response(http.StatusBadRequest, "Unknown Navigator"), nil
	}
	return Response(http.StatusOK, navigatorRequest.Navigator), nil
}

func (s *DefaultApiService) FindShipRoute(ctx context.Context, req ShipRouteRequest) (ImplResponse, error) {
	return s.ComputeRoute(ctx, RouteRequest{Origin: req.Origin, Destination: req.Destination})
}

func (s *DefaultApiService) FindRoadRoute(ctx context.Context, req RoadRouteRequest) (ImplResponse, error) {
	origin := geometry.NewPoint(float64(req.Origin.Lat), float64(req.Origin.Lon))
	destination := geometry.NewPoint(float64(req.Destination.Lat), float64(req.Destination.Lon))

	routeConfig := routing.RouteConfig{
		VehicleType:     req.VehicleType,
		AvoidTolls:      req.AvoidTolls,
		PreferHighway:   req.PreferHighway,
		ConsiderTraffic: s.config.ConsiderTraffic,
		MaxSpeed:        getMaxSpeedForVehicle(req.VehicleType),
	}

	route := s.router.ComputeRoute(*origin, *destination, routeConfig)

	routeResult := RouteResult{
		Origin:      req.Origin,
		Destination: req.Destination,
		Reachable:   route.Exists,
	}

	if route.Exists {
		waypoints := make([]Point, 0)
		for _, waypoint := range route.Waypoints {
			p := Point{Lat: float32(waypoint.Lat()), Lon: float32(waypoint.Lon())}
			waypoints = append(waypoints, p)
		}
		routeResult.Path = Path{
			Length:    int32(route.Length),
			Waypoints: waypoints,
		}
	}

	return Response(http.StatusOK, routeResult), nil
}

func (s *DefaultApiService) GetTrafficInfo(ctx context.Context, req TrafficInfoRequest) (ImplResponse, error) {
	return Response(http.StatusNotImplemented, "Traffic info not implemented"), nil
}

// 获取不同车辆类型的最大速度限制
func getMaxSpeedForVehicle(vehicleType string) int {
	switch vehicleType {
	case "car":
		return 120 // 小汽车最高限速120km/h
	case "truck":
		return 100 // 卡车最高限速100km/h
	case "bus":
		return 100 // 公交车最高限速100km/h
	case "motorcycle":
		return 100 // 摩托车最高限速100km/h
	case "bicycle":
		return 30 // 自行车最高速度30km/h
	default:
		return 120 // 默认使用小汽车限速
	}
}
