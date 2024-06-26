---
title: Run tests
---

# How to run all e2e tests

1. Install HyperShift.
2. Run all tests:

        make e2e
        bin/test-e2e -test.v -test.timeout 0 \
        --e2e.aws-credentials-file /my/aws-credentials \
        --e2e.pull-secret-file /my/pull-secret \
        --e2e.base-domain my-basedomain \
        --e2e.aws-oidc-s3-bucket-name my-bucket-name

*Note* if the s3 bucket is not in the `us-east-1` region then you need to add the `--e2e.aws-region` flag.

# How to run e2e tests for the "None" platform

        make e2e
        bin/test-e2e -test.v -test.timeout 0 \
        --e2e.pull-secret-file /my/pull-secret \
        --e2e.base-domain my-basedomain -test.run='^TestNone.*'
