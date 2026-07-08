package enhancer

import (
	"context"
	"time"
)

type Config struct {
	WorkDir   string
	OutputDir string
}

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Job struct {
	ID           string
	Status       Status
	Mode         string
	Preset       string
	Format       string
	InputName    string
	InputPath    string
	OutputDir    string
	OutputName   string
	OutputPath   string
	Stage        string
	Progress     float64
	Logs         []LogLine
	Error        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	cancel       context.CancelFunc
	workDir      string
	requestedDir string
}

type LogLine struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

type JobSnapshot struct {
	ID         string    `json:"id"`
	Status     Status    `json:"status"`
	Mode       string    `json:"mode"`
	Preset     string    `json:"preset"`
	Format     string    `json:"format"`
	InputName  string    `json:"inputName"`
	OutputName string    `json:"outputName"`
	OutputPath string    `json:"outputPath"`
	Stage      string    `json:"stage"`
	Progress   float64   `json:"progress"`
	Logs       []LogLine `json:"logs"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func (j *Job) Snapshot() JobSnapshot {
	logs := make([]LogLine, len(j.Logs))
	copy(logs, j.Logs)
	return JobSnapshot{
		ID:         j.ID,
		Status:     j.Status,
		Mode:       j.Mode,
		Preset:     j.Preset,
		Format:     j.Format,
		InputName:  j.InputName,
		OutputName: j.OutputName,
		OutputPath: j.OutputPath,
		Stage:      j.Stage,
		Progress:   j.Progress,
		Logs:       logs,
		Error:      j.Error,
		CreatedAt:  j.CreatedAt,
		UpdatedAt:  j.UpdatedAt,
	}
}

type ToolStatus struct {
	Name      string `json:"name"`
	Found     bool   `json:"found"`
	Path      string `json:"path,omitempty"`
	Required  bool   `json:"required"`
	Message   string `json:"message"`
	Version   string `json:"version,omitempty"`
	Install   string `json:"install,omitempty"`
	IssueHint string `json:"issueHint,omitempty"`
}

type HealthResponse struct {
	ReadyForFast bool         `json:"readyForFast"`
	ReadyForAI   bool         `json:"readyForAI"`
	Tools        []ToolStatus `json:"tools"`
}
