package incidentapi

type Repository interface {
	CreateIncident(req CreateIncidentRequest) (*Incident, error)
	GetIncident(id string) (*Incident, error)
	ListIncidents(req ListIncidentsRequest) ([]Incident, error)
	ListEvents(id string) ([]IncidentEvent, error)
	ListAgentRuns(id string) ([]AgentRun, error)
	SaveAnalysis(id string, req UpsertAnalysisRequest) (*Incident, error)
	SaveReport(id string, req UpsertReportRequest) (*Incident, error)
	ReviewIncident(id, status string, req ReviewIncidentRequest) (*Incident, error)
	RecordEvent(id string, req RecordIncidentEventRequest) error
	RecordAgentRun(id string, req RecordAgentRunRequest) error
}
