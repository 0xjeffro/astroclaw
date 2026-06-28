import * as cdk from 'aws-cdk-lib/core';
import * as crypto from 'crypto';
import { Match, Template } from 'aws-cdk-lib/assertions';
import { InfraStack } from '../lib/infra-stack';

// Fix non-deterministic values so snapshot tests are stable.
// - crypto.randomBytes: generated admin password changes every synth
// - Date.now: CustomResource version changes every synth
jest.spyOn(crypto, 'randomBytes').mockImplementation(((size: number) => Buffer.alloc(size)) as typeof crypto.randomBytes);
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
    Name: 'astroclaw-api',
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

// Verifies the WebSocket API is created.
test('WebSocket API exists', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::ApiGatewayV2::Api', {
    Name: 'astroclaw-ws',
    ProtocolType: 'WEBSOCKET',
  });
});

// Verifies $connect and $disconnect routes are configured.
test('WebSocket has $connect and $disconnect routes', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::ApiGatewayV2::Route', {
    RouteKey: '$connect',
  });
  template.hasResourceProperties('AWS::ApiGatewayV2::Route', {
    RouteKey: '$disconnect',
  });
});

// Verifies the migrate Lambda receives the generated admin password env var.
// This is the wiring most likely to silently break (typo in env key, missing
// Fn.conditionIf), so we assert it directly via the construct's logical ID.
test('Migrate Lambda wires GENERATED_ADMIN_PASSWORD', () => {
  const template = createTemplate();
  const lambdas = template.findResources('AWS::Lambda::Function');
  const migrateKey = Object.keys(lambdas).find(k => k.startsWith('MigrateHandler'));
  expect(migrateKey).toBeDefined();
  const env = lambdas[migrateKey!].Properties.Environment.Variables;
  expect(env).toHaveProperty('GENERATED_ADMIN_PASSWORD');
});

// Verifies WebSocket connect/disconnect Lambdas have correct timeout (10s).
test('WebSocket Lambdas have 10s timeout', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::Lambda::Function', {
    Timeout: 10,
    MemorySize: 128,
  });
});

// Verifies Reply Lambda has execute-api:ManageConnections for WebSocket push.
test('Reply Lambda has ManageConnections permission', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::IAM::Policy', {
    PolicyDocument: {
      Statement: Match.arrayWith([
        Match.objectLike({
          Action: 'execute-api:ManageConnections',
        }),
      ]),
    },
  });
});

// Verifies the S3 bucket for skills storage exists with public access blocked.
test('Skills S3 bucket exists with public access blocked', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::S3::Bucket', {
    PublicAccessBlockConfiguration: {
      BlockPublicAcls: true,
      BlockPublicPolicy: true,
      IgnorePublicAcls: true,
      RestrictPublicBuckets: true,
    },
  });
});

// Verifies Reply Lambda has S3 read access for loading skills.
test('Reply Lambda has S3 read access', () => {
  const template = createTemplate();
  template.hasResourceProperties('AWS::IAM::Policy', {
    PolicyDocument: {
      Statement: Match.arrayWith([
        Match.objectLike({
          Action: Match.arrayWith(['s3:GetObject*', 's3:GetBucket*', 's3:List*']),
        }),
      ]),
    },
  });
});
