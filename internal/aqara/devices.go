package aqara

import "context"

const pageSize = 50

// Device is an entry of query.device.info.
type Device struct {
	DID             string `json:"did"`
	ParentDID       string `json:"parentDid"`
	PositionID      string `json:"positionId"`
	Model           string `json:"model"`
	ModelType       int    `json:"modelType"`
	State           int    `json:"state"` // 1 = online, 0 = offline
	FirmwareVersion string `json:"firmwareVersion"`
	DeviceName      string `json:"deviceName"`
}

func (d Device) Online() bool { return d.State == 1 }

// Position is a home / room (query.position.info).
type Position struct {
	PositionID       string `json:"positionId"`
	PositionName     string `json:"positionName"`
	ParentPositionID string `json:"parentPositionId"`
}

// Scene is an Aqara scene (query.scene.listByPositionId).
type Scene struct {
	SceneID string `json:"sceneId"`
	Name    string `json:"name"`
}

// ResourceValue is a single value of a device attribute (query.resource.value).
type ResourceValue struct {
	SubjectID  string `json:"subjectId"`
	ResourceID string `json:"resourceId"`
	Value      string `json:"value"`
	TimeStamp  int64  `json:"timeStamp"`
}

type page[T any] struct {
	Data       []T `json:"data"`
	TotalCount int `json:"totalCount"`
}

// paginate fetches all pages of a list intent.
func paginate[T any](ctx context.Context, c *Client, intent string, data map[string]any) ([]T, error) {
	var all []T
	for pageNum := 1; ; pageNum++ {
		data["pageNum"] = pageNum
		data["pageSize"] = pageSize
		var p page[T]
		if err := c.call(ctx, intent, data, &p); err != nil {
			return nil, err
		}
		all = append(all, p.Data...)
		if len(p.Data) == 0 || len(all) >= p.TotalCount {
			return all, nil
		}
	}
}

// ListDevices returns all devices of the authorized account.
func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	return paginate[Device](ctx, c, "query.device.info", map[string]any{})
}

// ListPositions returns child positions of parentID ("" = top level homes).
func (c *Client) ListPositions(ctx context.Context, parentID string) ([]Position, error) {
	return paginate[Position](ctx, c, "query.position.info", map[string]any{
		"parentPositionId": parentID,
	})
}

// ListScenes returns scenes of a position.
func (c *Client) ListScenes(ctx context.Context, positionID string) ([]Scene, error) {
	return paginate[Scene](ctx, c, "query.scene.listByPositionId", map[string]any{
		"positionId": positionID,
	})
}

// RunScene executes a scene.
func (c *Client) RunScene(ctx context.Context, sceneID string) error {
	return c.call(ctx, "config.scene.run", map[string]any{"sceneId": sceneID}, nil)
}

// ResourceQuery selects attributes of one device.
type ResourceQuery struct {
	SubjectID   string   `json:"subjectId"`
	ResourceIDs []string `json:"resourceIds"`
}

// ResourceName is the user-visible name of a device attribute, e.g. a relay
// channel name set in the Aqara Home app (query.resource.name).
type ResourceName struct {
	SubjectID  string `json:"subjectId"`
	ResourceID string `json:"resourceId"`
	Name       string `json:"name"`
}

// ReadResources reads attribute values of several devices in one call,
// e.g. "4.1.85" (relay channel 1 state) or "0.1.85" (temperature).
func (c *Client) ReadResources(ctx context.Context, queries []ResourceQuery) ([]ResourceValue, error) {
	var out []ResourceValue
	err := c.call(ctx, "query.resource.value", map[string]any{"resources": queries}, &out)
	return out, err
}

// ResourceNames returns attribute names (relay channel names) of devices.
func (c *Client) ResourceNames(ctx context.Context, dids []string) ([]ResourceName, error) {
	var out []ResourceName
	err := c.call(ctx, "query.resource.name", map[string]any{"subjectIds": dids}, &out)
	return out, err
}

// WriteResource sets a device attribute, e.g. "4.1.85" = "1" (switch on).
func (c *Client) WriteResource(ctx context.Context, did, resourceID, value string) error {
	return c.call(ctx, "write.resource.device", []map[string]any{{
		"subjectId": did,
		"resources": []map[string]string{{"resourceId": resourceID, "value": value}},
	}}, nil)
}
