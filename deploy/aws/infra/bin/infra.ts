#!/usr/bin/env node
import * as cdk from 'aws-cdk-lib/core';
import { InfraStack } from '../lib/infra-stack';

const app = new cdk.App();
// Stack name is configurable via STACK_NAME env so that ephemeral CI
// deploys (one stack per PR) do not collide. Defaults to "InfraStack"
// to preserve the historical name for the long-lived production stack.
//
// Examples:
//   # Long-lived production stack (default)
//   npx cdk deploy
//
//   # Per-PR ephemeral stack in CI (also set EPHEMERAL_STACK=true so
//   # the KMS pending window collapses to 7 days and the KMS alias is
//   # dropped to avoid cross-stack collision)
//   STACK_NAME=Astroclaw-pr-100 EPHEMERAL_STACK=true npx cdk deploy Astroclaw-pr-100
//
const stackName = process.env.STACK_NAME || 'InfraStack';
new InfraStack(app, stackName, {
  /* If you don't specify 'env', this stack will be environment-agnostic.
   * Account/Region-dependent features and context lookups will not work,
   * but a single synthesized template can be deployed anywhere. */

  /* Uncomment the next line to specialize this stack for the AWS Account
   * and Region that are implied by the current CLI configuration. */
  // env: { account: process.env.CDK_DEFAULT_ACCOUNT, region: process.env.CDK_DEFAULT_REGION },

  /* Uncomment the next line if you know exactly what Account and Region you
   * want to deploy the stack to. */
  // env: { account: '123456789012', region: 'us-east-1' },

  /* For more information, see https://docs.aws.amazon.com/cdk/latest/guide/environments.html */
});
