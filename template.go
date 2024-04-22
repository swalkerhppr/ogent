package ogent

import (
	"embed"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"text/template"

	"entgo.io/contrib/entoas"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
	"github.com/stoewer/go-strcase"
)

var (
	//go:embed template
	templateDir embed.FS
	// funcMap contains extra template functions used by ogent.
	funcMap = template.FuncMap{
		"convertTo":       convertTo,
		"eagerLoad":       eagerLoad,
		"edgeOperations":  entoas.EdgeOperations,
		"edgeViewName":    entoas.EdgeViewName,
		"fieldAnnotation": entoas.FieldAnnotation,
		"edgeAnnotation":  entoas.EdgeAnnotation,
		"hasParams":       hasParams,
		"hasRequestBody":  hasRequestBody,
		"httpRoute":       httpRoute,
		"httpVerb":        httpVerb,
		"isCreate":        isCreate,
		"isDelete":        isDelete,
		"isList":          isList,
		"isRead":          isRead,
		"isUpdate":        isUpdate,
		"kebab":           strcase.KebabCase,
		"nodeOperations":  entoas.NodeOperations,
		"ogenToEnt":       ogenToEnt,
		"replaceAll":      strings.ReplaceAll,
		"setFieldExpr":    setFieldExpr,
		"viewName":        entoas.ViewName,
		"viewNameEdge":    entoas.ViewNameEdge,
		"ogenStructField": ogenStructField,
	}
	// templates holds all templates used by ogent.
	templates = gen.MustParse(gen.NewTemplate("ogent").Funcs(funcMap).ParseFS(templateDir, "template/*tmpl"))
)

// eagerLoad returns the Go expression to eager load the required edges on the node operation.
func eagerLoad(n *gen.Type, op entoas.Operation) (string, error) {
	gs, err := entoas.GroupsForOperation(n.Annotations, op)
	if err != nil {
		return "", err
	}
	t, err := entoas.EdgeTree(n, gs)
	if err != nil {
		return "", err
	}
	if len(t) > 0 {
		es := make(Edges, len(t))
		for i, e := range t {
			es[i] = (*Edge)(e)
		}
		return es.entQuery(), nil
	}
	return "", nil
}

type (
	Edges []*Edge
	Edge  entoas.Edge
)

// entQuery runs entQuery on every Edge and appends them.
func (es Edges) entQuery() string {
	b := new(strings.Builder)
	for _, e := range es {
		b.WriteString(e.entQuery())
	}
	return b.String()
}

// EntQuery constructs the Go code to eager load all requested edges for the given one.
func (e Edge) entQuery() string {
	b := new(strings.Builder)
	fmt.Fprintf(b, ".%s(", strings.Title(e.EagerLoadField()))
	if len(e.Edges) > 0 {
		es := make(Edges, len(e.Edges))
		for i, e := range e.Edges {
			es[i] = (*Edge)(e)
		}
		fmt.Fprintf(
			b,
			"func (q *%s.%s) {\nq%s\n}",
			filepath.Base(e.Type.Config.Package),
			e.Type.QueryName(),
			es.entQuery(),
		)
	}
	b.WriteString(")")
	return b.String()
}

// hasParams returns if the given entoas.Operation has parameters.
func hasParams(op entoas.Operation) bool {
	return op != entoas.OpCreate
}

// hasRequestBody returns if the given entoas.Operation has a request body.
func hasRequestBody(op entoas.Operation) bool {
	return op == entoas.OpCreate || op == entoas.OpUpdate
}

// httpVerb returns the HTTP httpVerb for the given entoas.Operation.
func httpVerb(op entoas.Operation) (string, error) {
	switch op {
	case entoas.OpCreate:
		return http.MethodPost, nil
	case entoas.OpRead, entoas.OpList:
		return http.MethodGet, nil
	case entoas.OpUpdate:
		return http.MethodPatch, nil
	case entoas.OpDelete:
		return http.MethodDelete, nil
	}
	return "", fmt.Errorf("unknown operation: %q", op)
}

// httpRoute returns the HTTP endpoint for the given entoas.Operation.
func httpRoute(root string, op entoas.Operation) (string, error) {
	switch op {
	case entoas.OpCreate, entoas.OpList:
		return root, nil
	case entoas.OpRead, entoas.OpUpdate, entoas.OpDelete:
		return path.Join(root, "{id}"), nil
	}
	return "", fmt.Errorf("unknown operation: %q", op)
}

// isCreate returns if the given entoas.Operation is entoas.OpCreate.
func isCreate(op entoas.Operation) bool { return op == entoas.OpCreate }

// isRead returns if the given entoas.Operation is entoas.OpRead.
func isRead(op entoas.Operation) bool { return op == entoas.OpRead }

// isUpdate returns if the given entoas.Operation is entoas.OpUpdate.
func isUpdate(op entoas.Operation) bool { return op == entoas.OpUpdate }

// isDelete returns if the given entoas.Operation is entoas.OpDelete.
func isDelete(op entoas.Operation) bool { return op == entoas.OpDelete }

// isList returns if the given entoas.Operation is entoas.OpList.
func isList(op entoas.Operation) bool { return op == entoas.OpList }

// OAS Schema types.
const (
	Integer = "integer"
	Number  = "number"
	String  = "string"
	Boolean = "boolean"
)

// OAS Schema formats.
const (
	None     = ""
	UUID     = "uuid"
	Date     = "date"
	Time     = "time"
	DateTime = "date-time"
	Duration = "duration"
	URI      = "uri"
	IPv4     = "ipv4"
	IPv6     = "ipv6"
	Byte     = "byte"
	Password = "password"
	Int64    = "int64"
	Int32    = "int32"
	Float    = "float"
	Double   = "double"
)

// setFieldExpr returns a Go expression to set the field on a response.
func setFieldExpr(f *gen.Field, schema, rec, ident string) (string, error) {
	var rhs, lhs string
	rhs = fmt.Sprintf("%s.%s", ident, f.StructField())
	lhs = fmt.Sprintf("%s.%s", rec, ogenStructField(f))

	if f.IsEnum() {
		rhs = convertTo(schema+f.StructField(), rhs)
	}

	if !f.Optional && !f.Nillable {
		return fmt.Sprintf("%s = %s", lhs, castValues(f, rhs)), nil
	}

	t, err := entoas.OgenSchema(f)
	if err != nil {
		return "", err
	}

	// Enums need special handling.
	if f.IsEnum() {
		return fmt.Sprintf("NewOpt%s%s(%s)",
			schema, f.StructField(),
			convertTo(schema + f.StructField(), rhs),
		), nil
	}

	var opt string
	switch t.Type {
	case Integer:
		switch t.Format {
		case Int32:
			opt = "Int32"
		case Int64:
			opt = "Int64"
		case None:
			opt = "Int"
		default:
			return "", fmt.Errorf("unexpected type: %q", t.Format)
		}

	case Number:
		switch t.Format {
		case Float:
			opt = "Float32"
		case Double, None:
			opt = "Float64"
		case Int32:
			opt = "Int32"
		case Int64:
			opt = "Int64"
		default:
			return "", fmt.Errorf("unexpected type: %q", t.Format)
		}

	case String:
		switch t.Format {
		case Byte:
			return fmt.Sprintf("%s = %s", lhs, rhs), nil
		case DateTime:
			opt = "DateTime"
		case Date:
			opt = "Date"
		case Time:
			opt = "Time"			
		case Duration:
			opt = "Duration"
		case UUID:
			opt = "UUID"
		case IPv4, IPv6:
			opt = "IP"
		case URI:
			opt = "URL"
		case Password, None:
			opt = "String"
		default:
			return "", fmt.Errorf("unexpected type: %q", t.Format)
		}

	case Boolean:
		switch t.Format {
		case None:
			opt = "Bool"
		default:
			return "", fmt.Errorf("unexpected type: %q", t.Format)
		}

	default:
		return "", fmt.Errorf("unexpected type: %q", t.Format)
	}

	if f.Nillable {
		return fmt.Sprintf( "%s = Opt%s{}\nif %s != nil { %s.SetTo(*%s) }", lhs, opt, rhs, lhs, rhs ), nil
	} 

	return fmt.Sprintf( "%s = NewOpt%s(%s)", lhs, opt, castValues(f, rhs)), nil
}

func convertTo(typ, expr string) string {
	return fmt.Sprintf("%s(%s)", typ, expr)
}

func ogenToEnt(f *gen.Field, expr string) string {
	switch f.Type.Type {
	case field.TypeUint8:
		return fmt.Sprintf("uint8(%s)", expr)
	case field.TypeInt8:
		return fmt.Sprintf("int8(%s)", expr)
	case field.TypeUint16:
		return fmt.Sprintf("uint16(%s)", expr)
	case field.TypeInt16:
		return fmt.Sprintf("int16(%s)", expr)
	case field.TypeUint32:
		return fmt.Sprintf("uint32(%s)", expr)
	case field.TypeUint:
		return fmt.Sprintf("uint(%s)", expr)
	case field.TypeUint64:
		return fmt.Sprintf("uint64(%s)", expr)
	default:
		return expr
	}
}

func castValues(f *gen.Field, expr string) string {
	switch f.Type.Type {
	case field.TypeInt8, field.TypeUint8,
		field.TypeInt16, field.TypeUint16:
		return fmt.Sprintf("int32(%s)", expr)
	case field.TypeUint64, field.TypeUint32, field.TypeUint:
		return fmt.Sprintf("int64(%s)", expr) // TODO: possibly losing information here for uint64
	default:
		return expr
	}
}

// From Ogen 
var (
	rules = [...]string{
		"ACL", "API", "ASCII", "AWS", "CPU", "CSS", "DNS", "EOF", "GB", "GUID",
		"HTML", "HTTP", "HTTPS", "ID", "IP", "JSON", "KB", "LHS", "MAC", "MB",
		"QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH", "SSO", "TLS",
		"TTL", "UI", "UID", "URI", "URL", "UTF8", "UUID", "VM", "XML", "XMPP",
		"XSRF", "XSS", "SMS", "CDN", "TCP", "UDP", "DC", "PFS", "P2P",
		"SHA256", "SHA1", "MD5", "SRP", "2FA", "OAuth", "OAuth2",

		"PNG", "JPG", "GIF", "MP4", "WEBP",
	}
	// rulesMap is a map of lowered rules to their canonical form.
	rulesMap = func() (r map[string]string) {
		r = make(map[string]string)
		for _, v := range rules {
			r[strings.ToLower(v)] = v
		}
		return r
	}()
)

func ogenStructField(f *gen.Field) string {
	parts := strings.Split(f.Name, "_")
	result := ""
	for _, p := range parts {
		r, ok := rulesMap[strings.ToLower(p)]
		if ok {
			result += r
		} else {
			result += strcase.UpperCamelCase(p)
		}
	}
	return result
}
