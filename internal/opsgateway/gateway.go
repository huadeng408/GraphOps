package opsgateway

type GatewayStore interface {
	QueryChanges(req QueryRequest) (QueryResponse, error)
	QueryLogs(req QueryRequest) (QueryResponse, error)
	QueryDependencies(req QueryRequest) (QueryResponse, error)
	Rollback(req RollbackRequest) (RollbackResponse, error)
	Verify(req VerifyRequest) (VerificationResult, error)
}
