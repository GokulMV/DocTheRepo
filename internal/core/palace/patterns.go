package palace

import (
	"regexp"
	"strings"

	"github.com/GokulMV/DocTheRepo/internal/core/chunker"
)

// This file holds the extraction pattern tables (plan § 8.5). They are deliberately explicit lists so a
// reviewer can see exactly what counts as an endpoint, a topic, or a datastore.

type endpoint struct {
	method, route string
	line          int
}

// Router method names → HTTP method, for call-based routers (net/http, chi, gin, echo, gorilla, express,
// fastify, koa-router, axum). "Handle"/"HandleFunc"/"route"/"all"/"Any" are method-agnostic.
var routerMethods = map[string]string{
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE", "Patch": "PATCH", "Head": "HEAD", "Options": "OPTIONS",
	"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE", "PATCH": "PATCH", "HEAD": "HEAD", "OPTIONS": "OPTIONS",
	"get": "GET", "post": "POST", "put": "PUT", "delete": "DELETE", "patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
	"Handle": "ANY", "HandleFunc": "ANY", "Any": "ANY", "all": "ANY", "route": "ANY", "Route": "ANY", "api_route": "ANY",
	"Method": "ANY", "MethodFunc": "ANY",
}

// Annotation/decorator names → HTTP method (Spring, JAX-RS, FastAPI/Flask, NestJS, actix/rocket).
var annotationMethods = map[string]string{
	"GetMapping": "GET", "PostMapping": "POST", "PutMapping": "PUT", "DeleteMapping": "DELETE", "PatchMapping": "PATCH",
	"RequestMapping": "ANY",
	"get":            "GET", "post": "POST", "put": "PUT", "delete": "DELETE", "patch": "PATCH", "head": "HEAD", "options": "OPTIONS",
	"route": "ANY", "api_route": "ANY",
	"Get": "GET", "Post": "POST", "Put": "PUT", "Delete": "DELETE", "Patch": "PATCH", "All": "ANY",
}

// Class-level annotations that prefix method routes.
var prefixAnnotations = map[string]bool{"RequestMapping": true, "Controller": true, "Path": true, "RestController": false}

// JAX-RS: @Path("/x") on a method with @GET/@POST marker annotations.
var jaxrsMethods = map[string]string{"GET": "GET", "POST": "POST", "PUT": "PUT", "DELETE": "DELETE", "PATCH": "PATCH"}

func classRoutePrefixes(a *chunker.FileAnalysis) map[string]string {
	out := map[string]string{}
	for _, d := range a.Definitions {
		for _, an := range d.Annotations {
			if prefixAnnotations[lastSeg(an.Name)] && len(an.StringArgs) > 0 {
				out[d.Symbol] = an.StringArgs[0]
			}
		}
	}
	return out
}

func endpoints(language string, d chunker.Definition, classPrefix string) []endpoint {
	var out []endpoint
	// Annotation/decorator routes.
	var jaxPath string
	jaxMethod := ""
	for _, an := range d.Annotations {
		name := lastSeg(an.Name)
		if m, ok := jaxrsMethods[name]; ok && len(an.StringArgs) == 0 {
			jaxMethod = m
			continue
		}
		if name == "Path" && len(an.StringArgs) > 0 {
			jaxPath = an.StringArgs[0]
			continue
		}
		m, ok := annotationMethods[name]
		if !ok || prefixAnnotations[name] && isClassKind(d.Kind) {
			continue
		}
		route := ""
		if len(an.StringArgs) > 0 {
			route = an.StringArgs[0]
		}
		// Decorator routes in Python/TS must look like paths or be Nest-style relative segments on a
		// class with a @Controller prefix; this keeps @app.get("key") cache helpers out.
		if route != "" && !strings.HasPrefix(route, "/") && classPrefix == "" && language != "java" {
			continue
		}
		out = append(out, endpoint{method: m, route: joinRoute(classPrefix, route), line: an.Line})
	}
	if jaxMethod != "" {
		out = append(out, endpoint{method: jaxMethod, route: joinRoute(classPrefix, jaxPath), line: d.StartLine})
	}
	// Call-based routers.
	for _, c := range d.Calls {
		m, ok := routerMethods[c.Name]
		if !ok || len(c.StringArgs) == 0 || c.Callee == c.Name { // a bare get("/x") is too ambiguous
			continue
		}
		route := c.StringArgs[0]
		if (c.Name == "Handle" || c.Name == "HandleFunc") && strings.Contains(route, " /") { // Go 1.22 "GET /x"
			parts := strings.SplitN(route, " ", 2)
			m, route = strings.ToUpper(parts[0]), strings.TrimSpace(parts[1])
		}
		if !strings.HasPrefix(route, "/") {
			continue
		}
		out = append(out, endpoint{method: m, route: route, line: c.Line})
	}
	return out
}

func joinRoute(prefix, route string) string {
	if prefix == "" {
		if route == "" {
			return "/"
		}
		if !strings.HasPrefix(route, "/") {
			return "/" + route
		}
		return route
	}
	p := "/" + strings.Trim(prefix, "/")
	r := strings.Trim(route, "/")
	if r == "" {
		return p
	}
	return p + "/" + r
}

func isClassKind(k string) bool { return k == "class" || k == "interface" || k == "record" }

type topicUse struct {
	name, edge string
	line       int
}

// Messaging clients are recognised by a transport word in the receiver, so `res.send("ok")` is never a topic.
var transportWord = regexp.MustCompile(`(?i)(producer|publisher|consumer|subscriber|subscription|listener|reader|writer|kafka|pubsub|sns|sqs|topic|queue|bus|channel|rabbit|amqp|nats|jms|kinesis|eventbridge|stream|template|emitter|broker|client)`)

var (
	publishNames   = map[string]bool{"Publish": true, "publish": true, "Produce": true, "produce": true, "SendMessage": true, "send_message": true, "publish_message": true, "PublishMessage": true, "sendMessage": true, "convertAndSend": true, "PutRecord": true, "put_record": true, "putRecord": true}
	publishLoose   = map[string]bool{"send": true, "Send": true, "emit": true, "Emit": true, "Write": true, "WriteMessages": true}
	subscribeNames = map[string]bool{"Subscribe": true, "subscribe": true, "SubscribeTopics": true, "Consume": true, "consume": true, "ReceiveMessage": true, "receive_message": true, "receiveMessage": true}
)

// Listener annotations: Spring Kafka/Rabbit/JMS, Spring Cloud AWS SQS, Micronaut, NestJS microservices.
var listenerAnnotations = map[string]bool{
	"KafkaListener": true, "RabbitListener": true, "SqsListener": true, "JmsListener": true, "Topic": true,
	"EventPattern": true, "MessagePattern": true, "StreamListener": true, "Incoming": true,
}

func topics(d chunker.Definition) []topicUse {
	var out []topicUse
	for _, an := range d.Annotations {
		if listenerAnnotations[lastSeg(an.Name)] {
			for _, s := range an.StringArgs {
				if validTopic(s) {
					out = append(out, topicUse{name: s, edge: EdgeSubscribes, line: an.Line})
				}
			}
		}
		if lastSeg(an.Name) == "Outgoing" || lastSeg(an.Name) == "SendTo" {
			for _, s := range an.StringArgs {
				if validTopic(s) {
					out = append(out, topicUse{name: s, edge: EdgePublishes, line: an.Line})
				}
			}
		}
	}
	for _, c := range d.Calls {
		if len(c.StringArgs) == 0 || !validTopic(c.StringArgs[0]) {
			continue
		}
		recv := strings.TrimSuffix(c.Callee, c.Name)
		transport := transportWord.MatchString(recv)
		switch {
		case publishNames[c.Name] && (transport || recv == ""):
			out = append(out, topicUse{name: c.StringArgs[0], edge: EdgePublishes, line: c.Line})
		case publishLoose[c.Name] && transport:
			out = append(out, topicUse{name: c.StringArgs[0], edge: EdgePublishes, line: c.Line})
		case subscribeNames[c.Name] && (transport || recv == ""):
			out = append(out, topicUse{name: c.StringArgs[0], edge: EdgeSubscribes, line: c.Line})
		}
	}
	return out
}

var topicRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/\-]{1,200}$`)

func validTopic(s string) bool {
	return topicRE.MatchString(s) && !strings.HasPrefix(s, "/") && !strings.Contains(s, "://") && !strings.Contains(s, " ")
}

// Env var name fragments → datastore type.
var envDatastores = []struct {
	re  *regexp.Regexp
	typ string
}{
	{regexp.MustCompile(`(?i)(POSTGRES|PGHOST|PG_|PGDATABASE)`), "postgres"},
	{regexp.MustCompile(`(?i)MYSQL`), "mysql"},
	{regexp.MustCompile(`(?i)MONGO`), "mongodb"},
	{regexp.MustCompile(`(?i)REDIS`), "redis"},
	{regexp.MustCompile(`(?i)DYNAMO`), "dynamodb"},
	{regexp.MustCompile(`(?i)(ELASTIC|OPENSEARCH)`), "elasticsearch"},
	{regexp.MustCompile(`(?i)CASSANDRA`), "cassandra"},
	{regexp.MustCompile(`(?i)(S3_BUCKET|BUCKET_NAME|_BUCKET$)`), "object_storage"},
	{regexp.MustCompile(`(?i)(DATABASE_URL|DB_URL|DB_HOST|DB_DSN|JDBC_URL|DATASOURCE_URL)`), "sql_database"},
}

func datastoreFromEnv(name string) string {
	for _, e := range envDatastores {
		if e.re.MatchString(name) {
			return e.typ
		}
	}
	return ""
}

// Client constructor callees → datastore type.
var callDatastores = []struct {
	re  *regexp.Regexp
	typ string
}{
	{regexp.MustCompile(`^(sql|sqlx|pgxpool|pgx)\.(Open|Connect|New|NewWithConfig|ConnectConfig)$`), "sql_database"},
	{regexp.MustCompile(`(?i)redis\.(NewClient|NewClusterClient|Redis|from_url|createClient)$|^createClient$`), "redis"},
	{regexp.MustCompile(`(?i)^mongo\.(Connect|NewClient)$|MongoClient$`), "mongodb"},
	{regexp.MustCompile(`(?i)^(psycopg2?|asyncpg)\.connect$|^create_engine$`), "sql_database"},
	{regexp.MustCompile(`(?i)dynamodb\.(New|NewFromConfig)$|DynamoDbClient|DynamoDBClient`), "dynamodb"},
	{regexp.MustCompile(`(?i)s3\.(New|NewFromConfig)$|S3Client$`), "object_storage"},
	{regexp.MustCompile(`(?i)^(elasticsearch|opensearch)\.(NewClient|NewDefaultClient)$|^Elasticsearch$`), "elasticsearch"},
}

func datastoreFromCall(c chunker.Call) string {
	if c.Callee == "boto3.client" || c.Callee == "boto3.resource" {
		if len(c.StringArgs) > 0 {
			switch c.StringArgs[0] {
			case "s3":
				return "object_storage"
			case "dynamodb":
				return "dynamodb"
			}
		}
		return ""
	}
	for _, d := range callDatastores {
		if d.re.MatchString(c.Callee) {
			return d.typ
		}
	}
	return ""
}

func lastSeg(s string) string {
	for _, sep := range []string{"::", "."} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	return s
}
