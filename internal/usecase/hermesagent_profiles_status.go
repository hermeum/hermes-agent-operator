package usecase

import (
	"context"
	"fmt"
	"slices"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"
)

// `Event` reasons for changes the operator applies to a profile.
const (
	eventReasonResumedCronJob = "ResumedCronJob"
	eventReasonPausedCronJob  = "PausedCronJob"

	eventActionResume = "Resume"
	eventActionPause  = "Pause"
)

// `applyProfileStatus` records what the operator has applied for each named
// profile.  It also records an `Event` for each distribution cron job that the
// next `Pod` start newly resumes or pauses.
//
// The `Event` objects describe the operator's decision, which comes from the
// spec.  The init container of the profile pauses and resumes the jobs when
// the `Pod` starts, and the operator cannot see the job store from here.  The
// operator compares the spec with the status that it wrote last, so it records
// one `Event` per change, not one per reconcile.  Like the init container, it
// skips jobs declared in crons, which belong to that step.
func (u *HermesAgentUseCase) applyProfileStatus(ctx context.Context, ha *agentsv1alpha1.HermesAgent) {
	previous := make(map[string][]string, len(ha.Status.Profiles))
	for _, p := range ha.Status.Profiles {
		previous[p.Name] = p.ResumedCrons
	}

	profiles := ha.GetHermes().GetProfiles()
	desired := make([]agentsv1alpha1.ProfileStatus, 0, len(profiles))

	for _, name := range ha.GetHermes().GetSortedProfileNames() {
		profile := profiles[name]
		declared := func(job string) bool {
			return slices.ContainsFunc(profile.Crons, func(c agentsv1alpha1.HermesCron) bool { return c.Name == job })
		}
		status := agentsv1alpha1.ProfileStatus{Name: name}
		if dist := profile.Distribution; dist != nil {
			status.Distribution = &agentsv1alpha1.ProfileDistributionStatus{
				Source: dist.GetSource(),
				Ref:    dist.GetRef(),
			}
			for _, job := range dist.GetResumeCrons() {
				if !declared(job) {
					status.ResumedCrons = append(status.ResumedCrons, job)
				}
			}
		}

		was := previous[name]
		for _, job := range status.ResumedCrons {
			if !slices.Contains(was, job) {
				u.recordCronJobEvent(ctx, ha, eventReasonResumedCronJob, eventActionResume, name, job,
					fmt.Sprintf("The next Pod start resumes cron job %q, which profile %q now allowlists", job, name))
			}
		}
		for _, job := range was {
			if !slices.Contains(status.ResumedCrons, job) && !declared(job) {
				u.recordCronJobEvent(ctx, ha, eventReasonPausedCronJob, eventActionPause, name, job,
					fmt.Sprintf("The next Pod start pauses cron job %q, which profile %q no longer allowlists", job, name))
			}
		}

		desired = append(desired, status)
	}

	// The init container deletes a profile that is removed from the spec.  Its
	// resumed jobs go with it, so they need no pause `Event`.
	ha.Status.Profiles = desired
}

func (u *HermesAgentUseCase) recordCronJobEvent(ctx context.Context, ha *agentsv1alpha1.HermesAgent, reason, action, profile, job, message string) {
	u.kube.RecordEvent(RecordEventParam{
		HermesAgent: ha,
		Type:        EventTypeNormal,
		Reason:      reason,
		Action:      action,
		Message:     message,
	})
	u.tel.Info(ctx, "Cron job state changed for HermesAgent", "reason", reason, "profile", profile, "job", job)
}
