
const fs = require('fs');
const path = require('path');

// Read the package.json file
const packageJsonPath = path.join(__dirname, '..', 'package.json');
const packageJson = JSON.parse(fs.readFileSync(packageJsonPath, 'utf8'));

// Add the test:api script if it doesn't exist
if (!packageJson.scripts['test:api']) {
  packageJson.scripts['test:api'] = 'cross-env NODE_ENV=test vitest run --config ./vitest.api.config.ts';
  
  // Write the updated package.json back to disk
  fs.writeFileSync(
    packageJsonPath,
    JSON.stringify(packageJson, null, 2) + '\n'
  );
  
  console.log('Added test:api script to package.json');
} else {
  console.log('test:api script already exists in package.json');
}
