package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tinkerbell/portal/src/pkg/redis"
	"github.com/tinkerbell/portal/src/pkg/types"
	"github.com/tinkerbell/tink/protos/workflow"
	"gopkg.in/yaml.v2"
)

// ListWorkflows returns a list of workflows
func ListWorkflows(ctx context.Context) ([]types.Workflow, error) {
	wfs, err := cache.GetAll(redis.CacheKeys.Workflows)
	if err != nil || wfs == nil || len(wfs) == 0 {
		return listWorkflowsFromServer(ctx)
	}
	workflows := []types.Workflow{}
	for id, wf := range wfs {
		var w types.Workflow
		if err := json.Unmarshal([]byte(wf), &w); err != nil {
			log.Error(err)
			cache.Delete(redis.CacheKeys.Workflows, id)
			continue
		}
		workflows = append(workflows, w)
	}
	return workflows, nil
}

// GetWorkflow returns details for the requested workflow ID
func GetWorkflow(ctx context.Context, id string, fillDetails bool) (types.Workflow, error) {
	result, err := cache.Get(redis.CacheKeys.Workflows, id)
	if err != nil || result == "" {
		wf, err := getWorkflow(ctx, id)
		if err != nil {
			return types.Workflow{}, err
		}
		if fillDetails {
			wf.Details = *parseWorkflowYAML(wf.RawData)
		}
		return wf, nil
	}

	var wf types.Workflow
	json.Unmarshal([]byte(result), &wf)
	if fillDetails {
		wf.Details = *parseWorkflowYAML(wf.RawData)
	}
	return wf, nil
}

// ParseWorkflowTemplate parses a template into workflow details
func ParseWorkflowTemplate(data string) *types.WorkflowDetails {
	return parseWorkflowYAML(data)
}

// CreateNewWorkflow creates a new workflow with
// selected template and hardware devices
func CreateNewWorkflow(ctx context.Context, templateID string, hardware string) (string, error) {
	res, err := workflowClient.CreateWorkflow(ctx, &workflow.CreateRequest{
		Template: templateID,
		Hardware: hardware,
	})
	if err != nil {
		return "", err
	}
	return res.Id, nil
}

func listWorkflowsFromServer(ctx context.Context) ([]types.Workflow, error) {
	res, err := workflowClient.ListWorkflows(ctx, &workflow.Empty{})
	if err != nil {
		return nil, err
	}
	workflows := []types.Workflow{}
	var wf *workflow.Workflow
	for wf, err = res.Recv(); err == nil && wf.Template != ""; wf, err = res.Recv() {
		w, err := getWorkflow(ctx, wf.GetId())
		if err == nil {
			w.CreatedAt = time.Unix(wf.GetCreatedAt().Seconds, 0).Local().Format(time.UnixDate)
			if err := cache.Set(redis.CacheKeys.Workflows, w.ID, w); err != nil {
				log.Error(err)
			}
			workflows = append(workflows, w)
		}
	}
	if err != nil && err != io.EOF {
		log.Fatal(err)
	}
	return workflows, nil
}

func getWorkflow(ctx context.Context, id string) (types.Workflow, error) {
	w, err := workflowClient.GetWorkflow(ctx, &workflow.GetRequest{Id: id})
	if err != nil {
		return types.Workflow{}, err
	}
	if w.Data == "" {
		return types.Workflow{}, fmt.Errorf("no data found for workflow ID: %v", id)
	}
	wf := types.Workflow{
		ID:      id,
		RawData: w.GetData(),
		State:   w.GetState().String(),
	}
	t, _ := cache.Get(redis.CacheKeys.TemplateNames, w.GetTemplate())
	json.Unmarshal([]byte(t), &wf.Template)
	setNameAndTimeout(&wf)
	cache.Set(redis.CacheKeys.Workflows, id, wf)
	return wf, nil
}

func setNameAndTimeout(wf *types.Workflow) {
	details := *parseWorkflowYAML(wf.RawData)
	wf.Name = details.Name
	wf.Timeout = strconv.Itoa(details.GlobalTimeout)
}

func parseWorkflowYAML(data string) *types.WorkflowDetails {
	var wf types.WorkflowDetails
	yaml.UnmarshalStrict([]byte(data), &wf)
	return &wf
}
