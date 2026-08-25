package agentjob

import (
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Handler handles AgentJob-related API requests.
type Handler struct {
	provider       AgentJobProvider
	scheduleParser ScheduleParser
	runner         AgentJobRunner
	logger         *zap.Logger
}

func NewHandler(provider AgentJobProvider, scheduleParser ScheduleParser, runner AgentJobRunner, logger *zap.Logger) *Handler {
	return &Handler{
		provider:       provider,
		scheduleParser: scheduleParser,
		runner:         runner,
		logger:         logger,
	}
}

func (h *Handler) RegisterRoutes(router *mux.Router) {
	agentJobRouter := router.PathPrefix("/agent-job").Subrouter()
	agentJobRouter.HandleFunc("", h.ListAgentJobs).Methods("GET")
	agentJobRouter.HandleFunc("", h.CreateAgentJob).Methods("POST")
	agentJobRouter.HandleFunc("/{id}", h.GetAgentJob).Methods("GET")
	agentJobRouter.HandleFunc("/{id}", h.UpdateAgentJob).Methods("PUT")
	agentJobRouter.HandleFunc("/{id}/status", h.UpdateAgentJobStatus).Methods("PUT")
	agentJobRouter.HandleFunc("/{id}/run", h.RunAgentJobNow).Methods("POST")
	agentJobRouter.HandleFunc("/{id}/rituals/{ritualId}", h.AddAgentJobRitual).Methods("POST")
	agentJobRouter.HandleFunc("/{id}/rituals/{ritualId}", h.RemoveAgentJobRitual).Methods("DELETE")
	agentJobRouter.HandleFunc("/{id}", h.DeleteAgentJob).Methods("DELETE")
	agentJobRouter.HandleFunc("/schedule/parse", h.ParseSchedule).Methods("POST")
}
