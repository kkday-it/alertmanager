package charts

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/wcharczuk/go-chart"
)

// AwsConfig struct.
type AwsConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	Key       string
}

// PrometheusQueryResult struct.
type PrometheusQueryResult struct {
	Metric map[string]interface{} `json:"metric"`
	Values [][]interface{}        `json:"values"`
}

// PrometheusQueryData struct.
type PrometheusQueryData struct {
	ResultType string                  `json:"resultType"`
	Result     []PrometheusQueryResult `json:"result"`
}

// PrometheusQuery struct.
type PrometheusQuery struct {
	Status string              `json:"status"`
	Data   PrometheusQueryData `json:"data"`
	Loc    *time.Location
}

// DrawCharts struct.
type DrawCharts struct {
	List []DrawChartRow
}

// DrawChartRow struct.
type DrawChartRow struct {
	Name    string
	XValues []time.Time
	YValues []float64
}

// GetPrometheusQueryAPI get prometheus query resutlt.
func GetPrometheusQueryAPI(r *http.Request, apiHostURL string) PrometheusQuery {
	query, ok := r.URL.Query()["query"]
	if !ok || len(query[0]) < 1 {
		panic("The query is empty.")
	}
	loc, ok := r.URL.Query()["loc"]
	if !ok || len(loc[0]) < 1 {
		loc = []string{"Asia/Taipei"}
	}
	return GetPrometheusQuery(query[0], loc[0], apiHostURL)
}

// GetPrometheusQuery get prometheus query resutlt.
func GetPrometheusQuery(query string, loc string, apiHostURL string) PrometheusQuery {

	startTime := strconv.FormatInt(time.Now().Add(-30*time.Minute).Unix(), 10)
	endTime := strconv.FormatInt(time.Now().Unix(), 10)

	fmt.Println("query", query)
	fmt.Println("loc", loc)
	fmt.Println("startTime", startTime)
	fmt.Println("endTime", endTime)
	resp, err := http.Get(apiHostURL + "/api/v1/query_range?step=1&start=" + startTime + "&end=" + endTime + "&query=" + url.QueryEscape(query))

	if err != nil {
		panic(err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("resp.Body", string(body))
		panic(err)
	}

	var prometheusQuery PrometheusQuery
	json.Unmarshal([]byte(string(body)), &prometheusQuery)
	if strings.Compare(prometheusQuery.Status, "error") == 0 {
		panic("error: " + string(body))
	}
	location, _ := time.LoadLocation(loc)
	prometheusQuery.Loc = location

	return prometheusQuery
}

// Draw chart return buffer.
func drawChart(drawCharts DrawCharts, prometheusQuery PrometheusQuery) *bytes.Buffer {
	var chartSeriesList []chart.Series
	for _, drawChartRow := range drawCharts.List {
		chartSeriesList = append(chartSeriesList, chart.TimeSeries{
			XValues: drawChartRow.XValues,
			YValues: drawChartRow.YValues,
			Name:    drawChartRow.Name,
			Style: chart.Style{
				Show:        true,
				StrokeColor: chart.GetDefaultColor(0).WithAlpha(64),
				FillColor:   chart.GetDefaultColor(0).WithAlpha(64),
			},
		})
	}

	graph := chart.Chart{
		Background: chart.Style{
			Padding: chart.Box{
				Top:    50,
				Left:   50,
				Right:  50,
				Bottom: 50,
			},
		},
		XAxis: chart.XAxis{
			Style: chart.Style{
				Show: true,
			},
			ValueFormatter: func(v interface{}) string {
				if typed, isTyped := v.(float64); isTyped {
					return time.Unix(0, int64(typed)).In(prometheusQuery.Loc).Format("01/02 15:04:05")
				}
				return ""
			},
			NameStyle: chart.Style{
				Show: true,
			},
			Name: "Time",
		},
		YAxis: chart.YAxis{
			Style: chart.Style{
				Show: true,
			},
			NameStyle: chart.Style{
				Show: true,
			},
			Name: "",
		},
		Series: chartSeriesList,
	}
	graph.Elements = []chart.Renderable{chart.LegendThin(&graph, chart.Style{
		Padding:  chart.Box{},
		FontSize: 12,
	})}
	buffer := bytes.NewBuffer([]byte{})
	err1 := graph.Render(chart.PNG, buffer)
	if err1 != nil {
		panic(err1)
	}

	return buffer
}

// UpdateImageS3 return s3 url.
func UpdateImageS3(source string, awsConfig *AwsConfig) string {
	sourceString := string(source)
	sourceByte := []byte(sourceString)
	hash := md5.Sum(sourceByte)
	awsConfig.Key = "prometheus/" + time.Now().Format("2006-01-02") + "/" + time.Now().Format("20060102150405") + "_" + hex.EncodeToString(hash[:]) + ".png"

	// Configure to use S3 Server
	s3Config := &aws.Config{
		Credentials: credentials.NewStaticCredentials(awsConfig.AccessKey, awsConfig.SecretKey, ""),
		Region:      aws.String(awsConfig.Region),
	}

	newSession := session.New(s3Config)
	uploader := s3manager.NewUploader(newSession)
	// Upload the file to S3.
	f := strings.NewReader(sourceString)
	result, err := uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(awsConfig.Bucket),
		Key:         aws.String(awsConfig.Key),
		Body:        f,
		ContentType: aws.String(http.DetectContentType(sourceByte)),
	})
	if err != nil {
		fmt.Printf("Failed to upload data to %s/%s, %s\n", awsConfig.Bucket, awsConfig.Key, err.Error())
		panic(err)
	}
	return result.Location
}

// GetCharts get source buffer.
func GetCharts(prometheusQuery PrometheusQuery) *bytes.Buffer {
	var drawCharts DrawCharts
	for _, res := range prometheusQuery.Data.Result {
		var drawChartRow DrawChartRow
		for _, value := range res.Values {
			tm := time.Unix(int64(value[0].(float64)), 0)
			drawChartRow.XValues = append(drawChartRow.XValues, tm)
			f, _ := strconv.ParseFloat(value[1].(string), 64)
			if math.IsNaN(f) {
				drawChartRow.YValues = append(drawChartRow.YValues, 0)
			} else {
				drawChartRow.YValues = append(drawChartRow.YValues, f)
			}
		}

		var names []string
		var keys []string
		for metricKey, _ := range res.Metric {
			keys = append(keys, metricKey)
		}
		sort.Strings(keys)
		for _, metricKey := range keys {
			names = append(names, metricKey+":"+res.Metric[metricKey].(string))
		}
		drawChartRow.Name = strings.Join(names, " ")
		drawCharts.List = append(drawCharts.List, drawChartRow)
	}

	return drawChart(drawCharts, prometheusQuery)
}

// LimitHandler http.
func LimitHandler() http.Handler {
	concLimiter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var prometheusQuery = GetPrometheusQueryAPI(r, "http://127.0.0.1:9090")
		buffer := GetCharts(prometheusQuery)
		w.Write(buffer.Bytes())
	})
	return concLimiter
}
