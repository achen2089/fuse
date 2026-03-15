package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"fuse/internal/domain"
)

type Client struct {
	baseURL string
	http    *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

func (c *Client) GetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp.Body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) PostJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return decodeError(resp.Body)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Status(ctx context.Context) (domain.ClusterStatus, error) {
	var status domain.ClusterStatus
	err := c.GetJSON(ctx, "/v1/status", &status)
	return status, err
}

func (c *Client) Nodes(ctx context.Context) ([]domain.Node, []domain.Device, error) {
	var payload struct {
		Nodes   []domain.Node   `json:"nodes"`
		Devices []domain.Device `json:"devices"`
	}
	err := c.GetJSON(ctx, "/v1/nodes", &payload)
	return payload.Nodes, payload.Devices, err
}

func (c *Client) Fabric(ctx context.Context) ([]domain.FabricLink, error) {
	var links []domain.FabricLink
	err := c.GetJSON(ctx, "/v1/fabric", &links)
	return links, err
}

func (c *Client) Teams(ctx context.Context) ([]domain.Team, error) {
	var teams []domain.Team
	err := c.GetJSON(ctx, "/v1/teams", &teams)
	return teams, err
}

func (c *Client) Jobs(ctx context.Context) ([]domain.Job, error) {
	var jobs []domain.Job
	err := c.GetJSON(ctx, "/v1/jobs", &jobs)
	return jobs, err
}

func (c *Client) Job(ctx context.Context, jobID string) (domain.Job, error) {
	var job domain.Job
	err := c.GetJSON(ctx, fmt.Sprintf("/v1/jobs/%s", jobID), &job)
	return job, err
}

func (c *Client) Events(ctx context.Context, limit int) ([]domain.Event, error) {
	var events []domain.Event
	path := "/v1/events"
	if limit > 0 {
		path = fmt.Sprintf("%s?limit=%d", path, limit)
	}
	err := c.GetJSON(ctx, path, &events)
	return events, err
}

func (c *Client) Storage(ctx context.Context, target string) (domain.StorageStatus, error) {
	var status domain.StorageStatus
	path := "/v1/storage"
	if target != "" {
		path = fmt.Sprintf("%s?path=%s", path, url.QueryEscape(target))
	}
	err := c.GetJSON(ctx, path, &status)
	return status, err
}

func (c *Client) Topology(ctx context.Context, req domain.TopologyRequest) (domain.TopologyProbe, error) {
	var probe domain.TopologyProbe
	err := c.PostJSON(ctx, "/v1/topology/probe", req, &probe)
	return probe, err
}

func (c *Client) Shard(ctx context.Context, req domain.ShardRequest) (domain.ShardPlan, error) {
	var plan domain.ShardPlan
	err := c.PostJSON(ctx, "/v1/shard", req, &plan)
	return plan, err
}

func (c *Client) Logs(ctx context.Context, jobID, stream string, tailLines int) (domain.JobLog, error) {
	var logs domain.JobLog
	path := fmt.Sprintf("/v1/jobs/%s/logs?stream=%s", jobID, stream)
	if tailLines != 0 {
		path = fmt.Sprintf("%s&tail=%d", path, tailLines)
	}
	err := c.GetJSON(ctx, path, &logs)
	return logs, err
}

func (c *Client) Submit(ctx context.Context, spec domain.JobSpec) (domain.Job, error) {
	var job domain.Job
	err := c.PostJSON(ctx, "/v1/jobs", spec, &job)
	return job, err
}

func (c *Client) Cancel(ctx context.Context, jobID string) error {
	return c.PostJSON(ctx, fmt.Sprintf("/v1/jobs/%s/cancel", jobID), map[string]any{}, nil)
}

func (c *Client) Checkpoint(ctx context.Context, jobID string) error {
	return c.PostJSON(ctx, fmt.Sprintf("/v1/jobs/%s/checkpoint", jobID), map[string]any{}, nil)
}

func (c *Client) Checkpoints(ctx context.Context, jobID string) ([]domain.Checkpoint, error) {
	var checkpoints []domain.Checkpoint
	err := c.GetJSON(ctx, fmt.Sprintf("/v1/checkpoints?job_id=%s", url.QueryEscape(jobID)), &checkpoints)
	return checkpoints, err
}

func (c *Client) Why(ctx context.Context, jobID string) (domain.Why, error) {
	var why domain.Why
	err := c.GetJSON(ctx, fmt.Sprintf("/v1/jobs/%s/why", jobID), &why)
	return why, err
}

func (c *Client) Simulate(ctx context.Context, req domain.SimulationRequest) (domain.SimulationResult, error) {
	var result domain.SimulationResult
	err := c.PostJSON(ctx, "/v1/simulations", req, &result)
	return result, err
}

func decodeError(r io.Reader) error {
	var payload map[string]string
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return err
	}
	if payload["error"] == "" {
		return errors.New("request failed")
	}
	return errors.New(payload["error"])
}
