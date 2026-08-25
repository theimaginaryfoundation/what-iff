package models

// EvaluationCriterion represents a single evaluation criterion with score and justification
type EvaluationCriterion struct {
	Name          string `json:"name" jsonschema:"required"`
	Score         int    `json:"score" jsonschema:"required,minimum=0,maximum=3"`
	Justification string `json:"justification" jsonschema:"required"`
}

// EvaluationResult represents the complete evaluation of a model response
type EvaluationResult struct {
	// Correctness & Accuracy
	FactualAccuracy  EvaluationCriterion `json:"factual_accuracy" jsonschema:"required"`
	LogicalCoherence EvaluationCriterion `json:"logical_coherence" jsonschema:"required"`

	// Depth & Nuance
	SMEDepth                       EvaluationCriterion `json:"sme_depth" jsonschema:"required"`
	NuanceContextualAwareness      EvaluationCriterion `json:"nuance_contextual_awareness" jsonschema:"required"`
	MultiDimensionalProblemSolving EvaluationCriterion `json:"multi_dimensional_problem_solving" jsonschema:"required"`

	// Practicality & Actionability
	ActionabilitySpecificity EvaluationCriterion `json:"actionability_specificity" jsonschema:"required"`
	FeasibilityRealism       EvaluationCriterion `json:"feasibility_realism" jsonschema:"required"`

	// Structure & Presentation
	StructureOrganization EvaluationCriterion `json:"structure_organization" jsonschema:"required"`
	ClarityConciseness    EvaluationCriterion `json:"clarity_conciseness" jsonschema:"required"`

	// Completeness & Iteration
	CompletenessOfResponse EvaluationCriterion `json:"completeness_of_response" jsonschema:"required"`

	// Optional for assistant agents only
	MemoryIterativeRefinement *EvaluationCriterion `json:"memory_iterative_refinement,omitempty" jsonschema:"required=false"`

	// Summary metrics
	TotalScore   int     `json:"total_score" jsonschema:"required"`
	AverageScore float64 `json:"average_score" jsonschema:"required"`
	MaxPossible  int     `json:"max_possible" jsonschema:"required"`
}

// ComparisonAnalysis represents the comparative analysis between two models
type ComparisonAnalysis struct {
	ModelAAnalysis     ModelAnalysis      `json:"model_a_analysis" jsonschema:"required"`
	ModelBAnalysis     ModelAnalysis      `json:"model_b_analysis" jsonschema:"required"`
	ComparativeSummary ComparativeSummary `json:"comparative_summary" jsonschema:"required"`
	Recommendation     Recommendation     `json:"recommendation" jsonschema:"required"`
	FinalSummary       FinalSummary       `json:"final_summary" jsonschema:"required"`
}

// ModelAnalysis represents the strengths and weaknesses of a model
type ModelAnalysis struct {
	Strengths  []string `json:"strengths" jsonschema:"required"`
	Weaknesses []string `json:"weaknesses" jsonschema:"required"`
}

// ComparativeSummary represents the key differences between the models
type ComparativeSummary struct {
	KeyDifferences     []string `json:"key_differences" jsonschema:"required"`
	ModelAOutperformed []string `json:"model_a_outperformed" jsonschema:"required"`
	ModelBOutperformed []string `json:"model_b_outperformed" jsonschema:"required"`
	SimilarPerformance []string `json:"similar_performance" jsonschema:"required"`
	CriticalGaps       []string `json:"critical_gaps" jsonschema:"required"`
	OverallAssessment  string   `json:"overall_assessment" jsonschema:"required"`
}

// Recommendation represents which model performed better and why
type Recommendation struct {
	BetterModel   string `json:"better_model" jsonschema:"required,enum=model_a,enum=model_b,enum=neither_clearly_superior"`
	Justification string `json:"justification" jsonschema:"required"`
}

// FinalSummary represents the overarching insights from the comparison
type FinalSummary struct {
	Summary     string `json:"summary" jsonschema:"required"`
	KeyTakeaway string `json:"key_takeaway" jsonschema:"required"`
}
