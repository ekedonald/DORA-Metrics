package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v45/github"
	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/oauth2"
)

type DoraMetrics struct {
	DeploymentFrequency   float64
	LeadTimeForChanges    float64
	TimeToRestoreService  float64
	ChangeFailureRate     float64
	SuccessfulDeployments int
	FailedDeployments     int
	Branch                string
}

var (
	deploymentFrequency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_deployment_frequency",
		Help: "Deployment Frequency metric",
	}, []string{"branch"})
	leadTimeForChanges = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_lead_time_for_changes_minutes",
		Help: "Lead Time for Changes metric (in minutes)",
	}, []string{"branch"})
	timeToRestoreService = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_time_to_restore_service",
		Help: "Time to Restore Service metric",
	}, []string{"branch"})
	changeFailureRate = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_change_failure_rate",
		Help: "Change Failure Rate metric",
	}, []string{"branch"})
	successfulDeployments = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_successful_deployments",
		Help: "Number of successful deployments in the last 30 days",
	}, []string{"branch"})
	failedDeployments = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dora_failed_deployments",
		Help: "Number of failed deployments in the last 30 days",
	}, []string{"branch"})
)

func init() {
	prometheus.MustRegister(deploymentFrequency)
	prometheus.MustRegister(leadTimeForChanges)
	prometheus.MustRegister(timeToRestoreService)
	prometheus.MustRegister(changeFailureRate)
	prometheus.MustRegister(successfulDeployments)
	prometheus.MustRegister(failedDeployments)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Println("scanning .env file for environment variables")
	}

	token := os.Getenv("GITHUB_TOKEN")
	webhookSecret := os.Getenv("WEBHOOK_SECRET")

	if token == "" || webhookSecret == "" {
		log.Fatal("GITHUB_TOKEN and WEBHOOK_SECRET must be set")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if err := github.ValidateSignature(r.Header.Get("X-Hub-Signature"), payload, []byte(webhookSecret)); err != nil {
			log.Printf("Error validating payload: %v", err)
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		event, err := github.ParseWebHook(github.WebHookType(r), payload)
		if err != nil {
			log.Printf("Error parsing webhook: %v", err)
			http.Error(w, "Error parsing webhook", http.StatusBadRequest)
			return
		}

		switch e := event.(type) {
		case *github.PushEvent:
			log.Printf("Received PushEvent for %s on branch %s", e.Repo.GetFullName(), e.GetRef())
			handleMetricsUpdate(client, e.Repo.GetFullName(), getBranchFromRef(e.GetRef()), w)
		case *github.WorkflowRunEvent:
			log.Printf("Received WorkflowRunEvent for %s on branch %s", e.Repo.GetFullName(), e.WorkflowRun.GetHeadBranch())
			handleMetricsUpdate(client, e.Repo.GetFullName(), e.WorkflowRun.GetHeadBranch(), w)
		case *github.PingEvent:
			w.Write([]byte("Pong!"))
		case *github.CheckRunEvent:
			log.Printf("Received CheckRunEvent for %s on branch %s", e.Repo.GetFullName(), e.CheckRun.GetCheckSuite().GetHeadBranch())
		case *github.CheckSuiteEvent:
			log.Printf("Received CheckSuiteEvent for %s on branch %s", e.Repo.GetFullName(), e.CheckSuite.GetHeadBranch())
		default:
			log.Printf("Received unhandled event type: %s", github.WebHookType(r))
		}
	})

	http.Handle("/metrics", promhttp.Handler())

	log.Println("Server is running on :4040")
	log.Fatal(http.ListenAndServe(":4040", nil))
}

func handleMetricsUpdate(client *github.Client, repoFullName string, branch string, w http.ResponseWriter) {
	metrics, err := calculateDoraMetrics(client, repoFullName, branch)
	if err != nil {
		log.Printf("Error calculating DORA metrics: %v", err)
		http.Error(w, "Error calculating DORA metrics", http.StatusInternalServerError)
		return
	}
	updatePrometheusMetrics(metrics)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		log.Printf("Error encoding metrics to JSON: %v", err)
	}
}

func calculateDoraMetrics(client *github.Client, repoFullName string, branch string) (*DoraMetrics, error) {
	log.Printf("Calculating DORA metrics for %s on branch %s", repoFullName, branch)

	deploymentFreq, successfulDeps, failedDeps := calculateDeploymentFrequency(client, repoFullName, branch)
	leadTime := calculateLeadTimeForChanges(client, repoFullName, branch)
	restoreTime := calculateTimeToRestoreService(client, repoFullName, branch)
	failureRate := calculateChangeFailureRate(client, repoFullName, branch)

	metrics := &DoraMetrics{
		DeploymentFrequency:   deploymentFreq,
		LeadTimeForChanges:    leadTime,
		TimeToRestoreService:  restoreTime,
		ChangeFailureRate:     failureRate,
		SuccessfulDeployments: successfulDeps,
		FailedDeployments:     failedDeps,
		Branch:                branch,
	}

	return metrics, nil
}

func calculateDeploymentFrequency(client *github.Client, repoFullName string, branch string) (float64, int, int) {
	log.Printf("Calculating Deployment Frequency for %s on branch %s", repoFullName, branch)

	workflowRuns, _, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), getOwner(repoFullName), getRepo(repoFullName), &github.ListWorkflowRunsOptions{
		Branch:      branch,
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		log.Printf("Error fetching workflow runs: %v", err)
		return 0, 0, 0
	}

	successfulDeployments := 0
	failedDeployments := 0
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	for _, run := range workflowRuns.WorkflowRuns {
		if run.GetCreatedAt().Time.After(thirtyDaysAgo) {
			if run.GetConclusion() == "success" {
				successfulDeployments++
			} else {
				failedDeployments++
			}
		}
	}

	frequency := float64(successfulDeployments+failedDeployments) / 30
	log.Printf("Calculated Deployment Frequency: %f", frequency)
	return frequency, successfulDeployments, failedDeployments
}

func calculateLeadTimeForChanges(client *github.Client, repoFullName string, branch string) float64 {
	log.Printf("Calculating Lead Time for Changes for %s on branch %s", repoFullName, branch)

	workflowRuns, _, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), getOwner(repoFullName), getRepo(repoFullName), &github.ListWorkflowRunsOptions{
		Status:      "success",
		Branch:      branch,
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		log.Printf("Error fetching workflow runs: %v", err)
		return 0
	}

	var totalLeadTime float64
	var count int
	for _, run := range workflowRuns.WorkflowRuns {
		if run.CreatedAt != nil && run.UpdatedAt != nil && run.CreatedAt.After(time.Now().AddDate(0, 0, -30)) {
			leadTime := run.UpdatedAt.Time.Sub(run.CreatedAt.Time).Minutes()
			totalLeadTime += leadTime
			count++
		}
	}

	if count == 0 {
		return 0
	}
	avgLeadTime := totalLeadTime / float64(count)
	log.Printf("Calculated Lead Time for Changes: %.2f minutes", avgLeadTime)
	return avgLeadTime
}

func calculateTimeToRestoreService(client *github.Client, repoFullName string, branch string) float64 {
	log.Printf("Calculating Time to Restore Service for %s on branch %s", repoFullName, branch)

	issues, _, err := client.Issues.ListByRepo(context.Background(), getOwner(repoFullName), getRepo(repoFullName), &github.IssueListByRepoOptions{
		State:       "closed",
		Labels:      []string{"incident"},
		Since:       time.Now().AddDate(0, 0, -30),
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		log.Printf("Error fetching issues: %v", err)
		return 0
	}

	totalRestoreTime := 0.0
	incidentCount := 0
	for _, issue := range issues {
		// Check if the issue is related to the specified branch
		if strings.Contains(issue.GetBody(), branch) {
			restoreTime := issue.GetClosedAt().Sub(issue.GetCreatedAt()).Hours()
			totalRestoreTime += restoreTime
			incidentCount++
		}
	}

	if incidentCount == 0 {
		return 0
	}
	avgRestoreTime := totalRestoreTime / float64(incidentCount)
	log.Printf("Calculated Time to Restore Service: %f hours", avgRestoreTime)
	return avgRestoreTime
}

func calculateChangeFailureRate(client *github.Client, repoFullName string, branch string) float64 {
	log.Printf("Calculating Change Failure Rate for %s on branch %s", repoFullName, branch)

	workflowRuns, _, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), getOwner(repoFullName), getRepo(repoFullName), &github.ListWorkflowRunsOptions{
		Branch:      branch,
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		log.Printf("Error fetching workflow runs: %v", err)
		return 0
	}

	totalDeployments := 0
	failedDeployments := 0
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	for _, run := range workflowRuns.WorkflowRuns {
		if run.GetCreatedAt().Time.After(thirtyDaysAgo) {
			totalDeployments++
			if run.GetConclusion() == "failure" {
				failedDeployments++
			}
		}
	}

	if totalDeployments == 0 {
		return 0
	}
	failureRate := float64(failedDeployments) / float64(totalDeployments)
	log.Printf("Calculated Change Failure Rate: %f", failureRate)
	return failureRate
}

func updatePrometheusMetrics(metrics *DoraMetrics) {
	deploymentFrequency.WithLabelValues(metrics.Branch).Set(metrics.DeploymentFrequency)
	leadTimeForChanges.WithLabelValues(metrics.Branch).Set(metrics.LeadTimeForChanges)
	timeToRestoreService.WithLabelValues(metrics.Branch).Set(metrics.TimeToRestoreService)
	changeFailureRate.WithLabelValues(metrics.Branch).Set(metrics.ChangeFailureRate)
	successfulDeployments.WithLabelValues(metrics.Branch).Set(float64(metrics.SuccessfulDeployments))
	failedDeployments.WithLabelValues(metrics.Branch).Set(float64(metrics.FailedDeployments))
}

func getOwner(repoFullName string) string {
	return strings.Split(repoFullName, "/")[0]
}

func getRepo(repoFullName string) string {
	return strings.Split(repoFullName, "/")[1]
}

func getBranchFromRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}
