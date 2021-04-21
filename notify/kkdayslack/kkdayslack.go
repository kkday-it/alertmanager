package kkdayslack

import (
	"fmt"

	charts "github.com/prometheus/alertmanager/api/charts"
	"github.com/prometheus/alertmanager/config"
)

func Notify(tmplText func(name string) (s string), conf *config.SlackConfig) string {
	query := tmplText(conf.ChartExpr)
	loc := tmplText(conf.ChartLoc)
	if len(query) > 0 {
		awsConfig := &charts.AwsConfig{
			AccessKey: tmplText(string(conf.AwsAccessKey)),
			SecretKey: tmplText(string(conf.AwsSecretKey)),
			Bucket:    tmplText(conf.AwsBucket),
			Region:    tmplText(conf.AwsRegion),
		}
		fmt.Println(query)
		var prometheusQuery = charts.GetPrometheusQuery(query, loc, tmplText(conf.PrometheusUrl))
		buffer := charts.GetCharts(prometheusQuery)
		if buffer == nil {
			return ""
		}
		url := charts.UpdateImageS3(buffer.String(), awsConfig)
		return url
	} else {
		return ""
	}
}
