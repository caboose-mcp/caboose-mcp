# packages/n8n/Dockerfile
#
# Extends the official n8n image to pre-load caboose-mcp workflows on first run.
# Workflows are imported via the n8n CLI (n8n import:workflow) in the entrypoint.
#
# If workflows already exist (n8n_data volume populated) the import is skipped.
# Activate workflows manually in the n8n UI after first run.

FROM docker.n8n.io/n8nio/n8n:latest

# Copy all workflow JSON files
COPY workflows/ /caboose-workflows/

# Copy the import entrypoint
COPY docker-entrypoint.sh /caboose-entrypoint.sh
RUN chmod +x /caboose-entrypoint.sh

ENTRYPOINT ["/caboose-entrypoint.sh"]
