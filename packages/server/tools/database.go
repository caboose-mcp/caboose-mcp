package tools

// database — PostgreSQL and MongoDB query tools.
//
// Connection strings are resolved from the request parameter first, then
// fall back to the POSTGRES_URL / MONGO_URL env vars set in config.
//
// Tools:
//   postgres_query            — execute arbitrary SQL against a PostgreSQL database
//   postgres_list_tables      — list all tables (excluding system schemas)
//   mongodb_query             — find documents in a MongoDB collection with an optional filter
//   mongodb_list_collections  — list collections in a MongoDB database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caboose-mcp/server/config"
	"github.com/jackc/pgx/v5"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

func RegisterDatabase(s *server.MCPServer, cfg *config.Config) {
	s.AddTool(mcp.NewTool("postgres_query",
		mcp.WithDescription("Execute a SQL query against a PostgreSQL database."),
		mcp.WithString("query", mcp.Required(), mcp.Description("SQL query to execute")),
		mcp.WithString("connection_string", mcp.Description("PostgreSQL connection string (falls back to POSTGRES_URL env var)")),
	), postgresQueryHandler(cfg))

	s.AddTool(mcp.NewTool("postgres_list_tables",
		mcp.WithDescription("List all tables in the PostgreSQL database."),
		mcp.WithString("connection_string", mcp.Description("PostgreSQL connection string (falls back to POSTGRES_URL env var)")),
	), postgresListTablesHandler(cfg))

	s.AddTool(mcp.NewTool("mongodb_query",
		mcp.WithDescription("Query a MongoDB collection."),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database name")),
		mcp.WithString("collection", mcp.Required(), mcp.Description("Collection name")),
		mcp.WithString("connection_string", mcp.Description("MongoDB connection string (falls back to MONGO_URL env var)")),
		mcp.WithString("query", mcp.Description("JSON filter document (default: {})")),
		mcp.WithNumber("limit", mcp.Description("Max documents to return (default 20)")),
	), mongodbQueryHandler(cfg))

	s.AddTool(mcp.NewTool("mongodb_list_collections",
		mcp.WithDescription("List collections in a MongoDB database."),
		mcp.WithString("database", mcp.Required(), mcp.Description("Database name")),
		mcp.WithString("connection_string", mcp.Description("MongoDB connection string (falls back to MONGO_URL env var)")),
	), mongodbListCollectionsHandler(cfg))
}

func postgresConnStr(cfg *config.Config, req mcp.CallToolRequest) (string, error) {
	if cs := req.GetString("connection_string", ""); cs != "" {
		return cs, nil
	}
	if cfg.PostgresURL != "" {
		return cfg.PostgresURL, nil
	}
	return "", fmt.Errorf("`postgres_query` and `postgres_list_tables` are not yet set up.\n\nTo configure them, set POSTGRES_URL=<postgres://user:pass@host/db> in your environment or .env file.\nAlternatively, pass connection_string directly in the tool call.")
}

func postgresQueryHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		connStr, err := postgresConnStr(cfg, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		conn, err := pgx.Connect(ctx, connStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connect error: %v", err)), nil
		}
		defer conn.Close(ctx)

		rows, err := conn.Query(ctx, query)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
		}
		defer rows.Close()

		var results []map[string]any
		fields := rows.FieldDescriptions()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("row error: %v", err)), nil
			}
			row := make(map[string]any, len(fields))
			for i, f := range fields {
				row[string(f.Name)] = vals[i]
			}
			results = append(results, row)
		}
		if err := rows.Err(); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("rows error: %v", err)), nil
		}

		out, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func postgresListTablesHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		connStr, err := postgresConnStr(cfg, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		conn, err := pgx.Connect(ctx, connStr)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connect error: %v", err)), nil
		}
		defer conn.Close(ctx)

		rows, err := conn.Query(ctx, `
			SELECT table_schema, table_name, table_type
			FROM information_schema.tables
			WHERE table_schema NOT IN ('pg_catalog','information_schema')
			ORDER BY table_schema, table_name`)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("query error: %v", err)), nil
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var schema, name, typ string
			if err := rows.Scan(&schema, &name, &typ); err != nil {
				continue
			}
			lines = append(lines, fmt.Sprintf("%s.%s (%s)", schema, name, typ))
		}
		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

func mongoConnStr(cfg *config.Config, req mcp.CallToolRequest) (string, error) {
	if cs := req.GetString("connection_string", ""); cs != "" {
		return cs, nil
	}
	if cfg.MongoURL != "" {
		return cfg.MongoURL, nil
	}
	return "", fmt.Errorf("`mongodb_query` and `mongodb_list_collections` are not yet set up.\n\nTo configure them, set MONGO_URL=<mongodb://user:pass@host/> in your environment or .env file.\nAlternatively, pass connection_string directly in the tool call.")
}

func mongodbQueryHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		connStr, err := mongoConnStr(cfg, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dbName, err := req.RequireString("database")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		collName, err := req.RequireString("collection")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		limit := int64(req.GetInt("limit", 20))

		var filter bson.D
		if q := req.GetString("query", ""); q != "" {
			if err := bson.UnmarshalExtJSON([]byte(q), true, &filter); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid query JSON: %v", err)), nil
			}
		} else {
			filter = bson.D{}
		}

		client, err := mongo.Connect(mongooptions.Client().ApplyURI(connStr))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connect error: %v", err)), nil
		}
		defer client.Disconnect(ctx)

		coll := client.Database(dbName).Collection(collName)
		opts := mongooptions.Find().SetLimit(limit)
		cur, err := coll.Find(ctx, filter, opts)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("find error: %v", err)), nil
		}
		defer cur.Close(ctx)

		var results []bson.M
		if err := cur.All(ctx, &results); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("decode error: %v", err)), nil
		}

		out, _ := json.MarshalIndent(results, "", "  ")
		return mcp.NewToolResultText(string(out)), nil
	}
}

func mongodbListCollectionsHandler(cfg *config.Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		connStr, err := mongoConnStr(cfg, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dbName, err := req.RequireString("database")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		client, err := mongo.Connect(mongooptions.Client().ApplyURI(connStr))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("connect error: %v", err)), nil
		}
		defer client.Disconnect(ctx)

		names, err := client.Database(dbName).ListCollectionNames(ctx, bson.D{})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list error: %v", err)), nil
		}
		return mcp.NewToolResultText(strings.Join(names, "\n")), nil
	}
}
