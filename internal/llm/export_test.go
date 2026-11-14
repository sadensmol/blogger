package llm

var (
	HandOver      = handOver
	ReadAgentFile = readAgentFile
)

const OutDirName = outDirName

func (r *Runner) NewJob(skill Skill) (Job, error) {
	return r.newJob(jobRequest{skill: skill, input: map[string]string{}})
}
