export interface PersonalityMediaJobResponse {
  job_id: string;
  job_type: 'expression_grid' | 'personality_portrait' | 'personality_generation';
}

export interface ActivePersonalityMediaJob {
  job_id: string;
  job_type: 'expression_grid' | 'personality_portrait' | 'personality_generation';
  reference: string;
  status: string;
  personality_id?: string;
  personality_name?: string;
  flow_id?: string;
  error?: string;
}

export interface PersonalityMediaJobConflict {
  message: string;
  active: ActivePersonalityMediaJob;
}
