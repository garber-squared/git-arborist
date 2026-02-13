#!/usr/bin/env node
/**
 * Development log server - receives logs from the browser and writes to file
 * Usage: node scripts/log-server.js
 * Logs are written to: logs/app.log
 */

import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PORT = 9999;
const LOG_DIR = path.join(__dirname, '..', 'logs');
const LOG_FILE = path.join(LOG_DIR, 'app.log');

// Ensure logs directory exists
if (!fs.existsSync(LOG_DIR)) {
  fs.mkdirSync(LOG_DIR, { recursive: true });
}

const logStream = fs.createWriteStream(LOG_FILE, { flags: 'a' });

// Also write startup message
const startMsg = `\n${'='.repeat(60)}\nLog server started at ${new Date().toISOString()}\n${'='.repeat(60)}\n`;
logStream.write(startMsg);
console.log(startMsg);

const server = http.createServer((req, res) => {
  // CORS headers for local development
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'POST, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');

  if (req.method === 'OPTIONS') {
    res.writeHead(204);
    res.end();
    return;
  }

  if (req.method === 'POST' && req.url === '/log') {
    let body = '';
    req.on('data', chunk => body += chunk);
    req.on('end', () => {
      try {
        const logEntry = JSON.parse(body);
        const formatted = formatLogEntry(logEntry);

        // Write to file
        logStream.write(formatted + '\n');

        // Also print to terminal with colors
        printColored(logEntry.level, formatted);

        res.writeHead(200, { 'Content-Type': 'application/json' });
        res.end(JSON.stringify({ success: true }));
      } catch (err) {
        console.error('Failed to parse log:', err);
        res.writeHead(400);
        res.end(JSON.stringify({ error: 'Invalid JSON' }));
      }
    });
  } else {
    res.writeHead(404);
    res.end('Not found');
  }
});

function formatLogEntry(entry) {
  const { timestamp, level, message, context, url } = entry;
  const contextStr = context ? ` | ${JSON.stringify(context)}` : '';
  const urlPath = url ? new URL(url).pathname : '';
  return `[${timestamp}] [${level.toUpperCase().padEnd(5)}] [${urlPath}] ${message}${contextStr}`;
}

function printColored(level, message) {
  const colors = {
    error: '\x1b[31m',   // red
    warn: '\x1b[33m',    // yellow
    info: '\x1b[36m',    // cyan
    debug: '\x1b[90m'    // gray
  };
  const reset = '\x1b[0m';
  console.log(`${colors[level] || ''}${message}${reset}`);
}

server.listen(PORT, () => {
  console.log(`📋 Log server listening on http://localhost:${PORT}`);
  console.log(`📁 Writing logs to: ${LOG_FILE}`);
  console.log(`\nTip: In another terminal, run: tail -f logs/app.log\n`);
});
