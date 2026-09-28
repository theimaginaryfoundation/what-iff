package datastore

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/telemetry"

	entgo "entgo.io/ent"
	"go.opentelemetry.io/otel/attribute"
)

// DB operation names for db.operation.name. ent queries and mutations use the first four;
// raw SQL statements use their own fixed names (dbOpVectorSearch...).
const (
	dbOpQuery  = "query"
	dbOpCreate = "create"
	dbOpUpdate = "update"
	dbOpDelete = "delete"

	dbOpVectorSearch        = "vector_search"
	dbOpAdvisoryLock        = "advisory_lock"
	dbOpAdvisoryLockCheck   = "advisory_lock_check"
	dbOpAdvisoryUnlock      = "advisory_unlock"
	dbCollectionSchedulerLk = "scheduler_lock"
)

// InstrumentEntClient records telemetry.DBOperationDuration for every ent query and mutation on
// client (and on transactions opened from it), labelled with the entity (db.collection.name,
// e.g. "chat_message") and operation (query, create, update, delete). Call it once, at startup,
// before the client is shared: ent appends interceptors, so calling it twice double-counts.
//
// One sample is one logical ent call, measured the way callers see it (scanning included):
//   - Eager-loaded edges (WithX) run inside their parent query and are part of its sample.
//   - A CreateBulk is one "create" sample: ent nests every builder's hooks on the caller's
//     context, so nested create hooks on the same context are folded into the outermost one.
//     The trade-off is that two creates running concurrently on one shared context can fold
//     into one sample; that undercounts slightly rather than inflating bulk inserts N-fold.
//   - ent's NotFound from First/Only is produced after the query returns, so an empty result is
//     a successful query. NotFound from UpdateOne/DeleteOne is recorded as error.type
//     not_found, and constraint violations as client_error.
func InstrumentEntClient(client *ent.Client, metrics *telemetry.Metrics) {
	if client == nil || metrics == nil {
		return
	}
	in := &entInstrumentation{metrics: metrics}
	client.Intercept(entgo.InterceptFunc(in.intercept))
	client.Use(in.hook)
}

type entInstrumentation struct {
	metrics *telemetry.Metrics

	mu       sync.Mutex
	mutating map[context.Context]struct{}
}

type dbOpCtxKey struct{}

func (in *entInstrumentation) intercept(next entgo.Querier) entgo.Querier {
	return entgo.QuerierFunc(func(ctx context.Context, q entgo.Query) (entgo.Value, error) {
		if ctx.Value(dbOpCtxKey{}) != nil {
			return next.Query(ctx, q)
		}
		start := time.Now()
		v, err := next.Query(context.WithValue(ctx, dbOpCtxKey{}, true), q)
		in.record(ctx, queryCollection(q), dbOpQuery, time.Since(start), err)
		return v, err
	})
}

func (in *entInstrumentation) hook(next entgo.Mutator) entgo.Mutator {
	return entgo.MutateFunc(func(ctx context.Context, m entgo.Mutation) (entgo.Value, error) {
		if ctx.Value(dbOpCtxKey{}) != nil || !in.enter(ctx) {
			return next.Mutate(ctx, m)
		}
		defer in.leave(ctx)
		start := time.Now()
		v, err := next.Mutate(context.WithValue(ctx, dbOpCtxKey{}, true), m)
		in.record(ctx, entityCollection(m.Type()), mutationOp(m.Op()), time.Since(start), err)
		return v, err
	})
}

// enter marks ctx as having a mutation in progress and reports whether this call is the
// outermost one. CreateBulk re-enters the hook chain with the caller's own context (not the
// one the outer hook passed down), so a context value can't detect that nesting. Entries only
// live for the duration of one mutation: the hook defers leave, so nothing is retained even if
// the mutation panics.
func (in *entInstrumentation) enter(ctx context.Context) bool {
	if !reflect.TypeOf(ctx).Comparable() {
		return true
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if _, busy := in.mutating[ctx]; busy {
		return false
	}
	if in.mutating == nil {
		in.mutating = map[context.Context]struct{}{}
	}
	in.mutating[ctx] = struct{}{}
	return true
}

func (in *entInstrumentation) leave(ctx context.Context) {
	if !reflect.TypeOf(ctx).Comparable() {
		return
	}
	in.mu.Lock()
	delete(in.mutating, ctx)
	in.mu.Unlock()
}

func (in *entInstrumentation) record(ctx context.Context, collection, op string, d time.Duration, err error) {
	attrs := []attribute.KeyValue{
		telemetry.AttrDBCollection.String(collection),
		telemetry.AttrDBOperation.String(op),
	}
	if et := entErrorType(op, err); et != "" {
		attrs = append(attrs, telemetry.AttrErrorType.String(et))
	}
	in.metrics.RecordDuration(ctx, telemetry.DBOperationDuration, d, attrs...)
}

func entErrorType(op string, err error) string {
	switch {
	case err == nil:
		return ""
	case ent.IsNotFound(err):
		if op == dbOpQuery {
			return ""
		}
		return telemetry.ErrorTypeNotFound
	case ent.IsConstraintError(err):
		return telemetry.ErrorTypeClient
	default:
		return telemetry.ClassifyError(err)
	}
}

func mutationOp(op entgo.Op) string {
	switch {
	case op.Is(entgo.OpCreate):
		return dbOpCreate
	case op.Is(entgo.OpDelete | entgo.OpDeleteOne):
		return dbOpDelete
	default:
		return dbOpUpdate
	}
}

var collectionNames sync.Map // reflect.Type or entity name -> snake_case collection

// queryCollection names the entity a query reads from its Go type (*ent.ChatMessageQuery ->
// "chat_message"), so the label set is bounded by the schema.
func queryCollection(q entgo.Query) string {
	t := reflect.TypeOf(q)
	if v, ok := collectionNames.Load(t); ok {
		return v.(string)
	}
	name := "other"
	if t != nil {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if base, ok := strings.CutSuffix(t.Name(), "Query"); ok && base != "" {
			name = snakeCase(base)
		}
	}
	collectionNames.Store(reflect.TypeOf(q), name)
	return name
}

// entityCollection turns a mutation's entity type ("ChatMessage") into "chat_message".
func entityCollection(entity string) string {
	if v, ok := collectionNames.Load(entity); ok {
		return v.(string)
	}
	name := "other"
	if entity != "" {
		name = snakeCase(entity)
	}
	collectionNames.Store(entity, name)
	return name
}

func snakeCase(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if unicode.IsUpper(r) {
			// Word boundary before an upper-case rune that follows a lower-case one, or that
			// starts a new word after an acronym ("MCPServer" -> "mcp_server").
			if i > 0 && (unicode.IsLower(runes[i-1]) ||
				(i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// timeRawSQL times one raw SQL statement on DBOperationDuration (a no-op when m is nil).
// collection is the main table and op a fixed statement name, never the SQL text.
func timeRawSQL(ctx context.Context, m *telemetry.Metrics, collection, op string) func(error) {
	if m == nil {
		return func(error) {}
	}
	return m.Time(ctx, telemetry.DBOperationDuration,
		telemetry.AttrDBCollection.String(collection),
		telemetry.AttrDBOperation.String(op))
}
