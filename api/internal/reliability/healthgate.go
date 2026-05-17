package reliability

import "fmt"

type SLO struct {
	Availability        float64 `json:"availability"`
	LatencyP99Ms        float64 `json:"latencyP99Ms"`
	ErrorRateThreshold  float64 `json:"errorRateThreshold"`
	MinimumSuccessCount int     `json:"minimumSuccessCount"`
}

type Metrics struct {
	P99LatencyMs float64 `json:"p99LatencyMs"`
	ErrorRate    float64 `json:"errorRate"`
	SuccessCount int     `json:"successCount"`
}

type Decision struct {
	Healthy bool   `json:"healthy"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
}

func DefaultSLO(target float64) SLO {
	return SLO{
		Availability:        target,
		LatencyP99Ms:        300,
		ErrorRateThreshold:  1,
		MinimumSuccessCount: 50,
	}
}

func EvaluateCanary(slo SLO, metrics Metrics) Decision {
	if metrics.SuccessCount < slo.MinimumSuccessCount {
		return Decision{
			Healthy: true,
			Action:  "continue",
			Reason:  fmt.Sprintf("waiting for more samples: %d/%d", metrics.SuccessCount, slo.MinimumSuccessCount),
		}
	}
	if metrics.P99LatencyMs > slo.LatencyP99Ms {
		return Decision{
			Healthy: false,
			Action:  "rollback",
			Reason:  fmt.Sprintf("p99 latency %.2fms exceeds %.2fms", metrics.P99LatencyMs, slo.LatencyP99Ms),
		}
	}
	if metrics.ErrorRate > slo.ErrorRateThreshold {
		return Decision{
			Healthy: false,
			Action:  "rollback",
			Reason:  fmt.Sprintf("error rate %.2f%% exceeds %.2f%%", metrics.ErrorRate, slo.ErrorRateThreshold),
		}
	}
	return Decision{Healthy: true, Action: "promote", Reason: "canary is within SLO thresholds"}
}
