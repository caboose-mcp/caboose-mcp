#!/usr/bin/env node

/**
 * Generate Bruno collection from live MCP endpoint
 * Usage: node sync-bruno-collection.js <baseUrl> <authToken> [output-dir]
 */

import fs from 'fs';
import path from 'path';
import https from 'https';

const args = process.argv.slice(2);
const baseUrl = args[0] || 'https://mcp.chrismarasco.io';
const authToken = args[1];
const outDir = args[2] || './bruno-generated';

if (!authToken) {
  console.error('Usage: node sync-bruno-collection.js <baseUrl> <authToken> [output-dir]');
  console.error('  baseUrl: MCP endpoint (default: https://mcp.chrismarasco.io)');
  console.error('  authToken: Bearer token for authentication');
  console.error('  output-dir: Output directory (default: ./bruno-generated)');
  process.exit(1);
}

async function fetchJSON(url, token, body) {
  return new Promise((resolve, reject) => {
    const urlObj = new URL(url);
    const options = {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${token}`,
        'Content-Type': 'application/json',
        'Content-Length': Buffer.byteLength(body),
      },
    };

    const req = https.request(urlObj, options, (res) => {
      let data = '';
      res.on('data', chunk => data += chunk);
      res.on('end', () => {
        try {
          resolve(JSON.parse(data));
        } catch (e) {
          reject(new Error(`Invalid JSON: ${data}`));
        }
      });
    });

    req.on('error', reject);
    req.write(body);
    req.end();
  });
}

function ensureDir(dir) {
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true });
  }
}

function generateBruFile(name, desc, counter) {
  const sanitizedDesc = desc.replace(/"/g, '\\"');
  return `meta {
  name: ${name}
  type: http
  seq: ${counter}
}

post {
  url: {{baseUrl}}/mcp
  body: json
  auth: bearer
}

auth:bearer {
  token: {{authToken}}
}

body:json {
  {
    "jsonrpc": "2.0",
    "id": ${counter},
    "method": "tools/call",
    "params": {
      "name": "${name}",
      "arguments": {}
    }
  }
}

docs {
  ${sanitizedDesc}
}
`;
}

async function main() {
  console.log(`Fetching tools from ${baseUrl}...`);

  try {
    const response = await fetchJSON(
      new URL('/mcp', baseUrl).toString(),
      authToken,
      JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'tools/list',
        params: {},
      })
    );

    if (response.error) {
      console.error('Error from MCP server:', response.error);
      process.exit(1);
    }

    const tools = response.result?.tools || [];
    console.log(`Found ${tools.length} tools`);

    ensureDir(outDir);

    const categories = new Set();
    let counter = 0;

    for (const tool of tools) {
      counter++;
      const { name, description } = tool;
      const category = name.split('_')[0];
      const categoryDir = path.join(outDir, category);

      ensureDir(categoryDir);
      categories.add(category);

      const bruFile = path.join(categoryDir, `${name}.bru`);
      fs.writeFileSync(bruFile, generateBruFile(name, description || 'No description', counter));
      console.log(`  ✓ Generated: ${category}/${name}.bru`);
    }

    // Create folder.bru files
    for (const category of categories) {
      const folderFile = path.join(outDir, category, 'folder.bru');
      fs.writeFileSync(folderFile, `meta {
  name: ${category}
  type: http
}
`);
      console.log(`  ✓ Created: ${category}/folder.bru`);
    }

    // Create environments
    ensureDir(path.join(outDir, 'environments'));

    fs.writeFileSync(path.join(outDir, 'environments', 'local.bru'), `vars {
  baseUrl: http://localhost:8080
  authToken: your-mcp-auth-token-here
}
`);

    fs.writeFileSync(path.join(outDir, 'environments', 'production.bru'), `vars {
  baseUrl: https://mcp.chrismarasco.io
  authToken: your-mcp-auth-token-here
}
`);

    console.log('  ✓ Created: environments/local.bru');
    console.log('  ✓ Created: environments/production.bru');

    // Create bruno.json
    fs.writeFileSync(path.join(outDir, 'bruno.json'), JSON.stringify({
      version: '1',
      name: 'caboose-mcp-generated',
      type: 'collection',
    }, null, 2));

    console.log('  ✓ Created: bruno.json');

    console.log('\n✅ Collection generated successfully!');
    console.log(`📁 Location: ${path.resolve(outDir)}`);
    console.log('\nTo use in Bruno:');
    console.log('  1. Open Bruno');
    console.log('  2. Click "Open Collection"');
    console.log(`  3. Select: ${path.resolve(outDir)}`);
    console.log('  4. Update auth token in environments');
  } catch (error) {
    console.error('Error:', error.message);
    process.exit(1);
  }
}

main();
