import * as cdk from 'aws-cdk-lib/core';
import { Template } from 'aws-cdk-lib/assertions';
import { InfraStack } from '../lib/infra-stack';

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

// Verifies the Migrate Lambda has a longer timeout.
test('Migrate Lambda exists with 5 min timeout', () => {
  const template = createTemplate();
  // Both Lambdas have 5 min timeout, so at least 2 should match.
  const resources = template.findResources('AWS::Lambda::Function', {
    Properties: {
      Timeout: 300,
    },
  });
  expect(Object.keys(resources).length).toBeGreaterThanOrEqual(2);
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
