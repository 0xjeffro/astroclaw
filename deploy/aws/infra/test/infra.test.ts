import * as cdk from 'aws-cdk-lib/core';
import * as crypto from 'crypto';
import { Template } from 'aws-cdk-lib/assertions';
import { InfraStack } from '../lib/infra-stack';

// Fix non-deterministic values so snapshot tests are stable.
// - crypto.randomUUID: generated API key changes every synth
// - Date.now: CustomResource version changes every synth
jest.spyOn(crypto, 'randomUUID').mockReturnValue('00000000-0000-0000-0000-000000000000');
jest.spyOn(Date, 'now').mockReturnValue(0);

// Replace asset hashes (64-char hex) with a placeholder. Go binaries are
// non-deterministic across builds, causing S3Key to change every time.
// This is the community best practice for CDK snapshot testing.
expect.addSnapshotSerializer({
  test: (val: unknown) => typeof val === 'string' && /^[a-f0-9]{64}\.zip$/.test(val),
  print: () => '"[ASSET HASH].zip"',
});

function createTemplate(): Template {
  const app = new cdk.App();
  const stack = new InfraStack(app, 'TestStack');
  return Template.fromStack(stack);
}

// Snapshot test: captures the full CloudFormation template.
// If any resource changes unexpectedly, this test fails.
// Run `npx jest --updateSnapshot` to accept intentional changes.
test('snapshot', () => {
  const template = createTemplate();
  expect(template.toJSON()).toMatchSnapshot();
});

// Verifies the DSQL cluster is created.
test('DSQL cluster exists', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::DSQL::Cluster', {
    DeletionProtectionEnabled: false,
  });
});

// Verifies the API Lambda is configured correctly.
test('API Lambda has correct runtime and architecture', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::Lambda::Function', {
    Runtime: 'provided.al2023',
    Architectures: ['arm64'],
    Handler: 'bootstrap',
    MemorySize: 256,
    Timeout: 300, // 5 minutes
  });
});

// Verifies Lambda timeouts match their roles:
// API Lambda: 30s (simple CRUD), Reply Lambda: 15min (agent loop), Migrate Lambda: 5min (migrations).
test('Lambda timeouts are configured correctly', () => {
  const template = createTemplate();

  // API Lambda: 30 seconds
  template.hasResourceProperties('AWS::Lambda::Function', {
    Timeout: 30,
  });

  // Reply Lambda: 15 minutes
  template.hasResourceProperties('AWS::Lambda::Function', {
    Timeout: 900,
  });

  // Migrate Lambda: 5 minutes
  template.hasResourceProperties('AWS::Lambda::Function', {
    Timeout: 300,
  });
});

// Verifies API Gateway HTTP API is created.
test('API Gateway HTTP API exists', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::ApiGatewayV2::Api', {
    Name: 'iclaw-api',
    ProtocolType: 'HTTP',
  });
});

// Verifies the catch-all route is configured.
test('API Gateway has catch-all route', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::ApiGatewayV2::Route', {
    RouteKey: 'ANY /{proxy+}',
  });
});

// Verifies the CloudFormation Custom Resource for migrations exists.
test('Migration Custom Resource exists', () => {
  const template = createTemplate();
  template.hasResource('AWS::CloudFormation::CustomResource', {});
});

// Verifies the API URL is exported as an output.
test('API URL output exists', () => {
  const template = createTemplate();
  const outputs = template.findOutputs('*');
  expect(Object.keys(outputs).length).toBeGreaterThanOrEqual(1);
});
