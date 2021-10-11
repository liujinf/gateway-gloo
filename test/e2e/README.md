# End-to-end tests
This directory contains end-to-end tests that do not require kubernetes

*Note: All commands should be run from the root directory of the Gloo repository*

## (Option A) Run Envoy in a Docker Container
### Setup
For these tests to run, we require Envoy be built in a docker container. The VERSION env variable determines the name of the tag for that image.

```bash
VERSION=<version-name> make gloo-envoy-wrapper-docker
```

### Run Tests
The `run-tests` make target runs ginkgo with a set of useful flags. The following environment variables can be configured for this target:

| Name            | Default | Description |
| ---             |   ---   |    ---      |
| ENVOY_IMAGE_TAG | ""      | The tag of the gloo-envoy-wrapper-docker image built during setup |
| TEST_PKG        | ""      | The path to the package of the test suite you want to run  |
| WAIT_ON_FAIL    | 0       | Set to 1 to prevent Ginkgo from cleaning up the Gloo Edge installation in case of failure. Useful to exec into inspect resources created by the test. A command to resume the test run (and thus clean up resources) will be logged to the output.

Example:
```bash
ENVOY_IMAGE_TAG=solo-test-image TEST_PKG=./test/e2e/... make run-tests
```

### Example Debug Workflow: Use `WAIT_ON_FAIL` to Dump Envoy Config
The `WAIT_ON_FAIL` environment variable can be used to inspect Gloo/Envoy state during an e2e test. Instructions to do so are as follows:
- Run your e2e test with `WAIT_ON_FAIL=1`, in order to prevent the Gloo installation from being torn down on failure
  - It is important that your test fails -- teardown will occur as normal if the test suite runs successfully
- When failure occurs, inspect running Docker containers using `docker ps`
  - You should see a container which matches the following criteria:
  - 
    |IMAGE|PORTS|NAMES|
    |-----|-----|-----|
    |quay.io/solo-io/gloo-envoy-wrapper:<ENVOY_IMAGE_TAG>|0.0.0.0:11082-11083->11082-11083/tcp, :::11082-11083->11082-11083/tcp, 0.0.0.0:21001->21001/tcp, :::21001->21001/tcp|e2e_envoy|
- Open http://0.0.0.0:21001 in your browser to access the envoy control panel
  - we default the adminPort to 20000 (https://github.com/solo-io/gloo/blob/master/test/services/envoy.go#L36), and when we create a new instance we add some digits to ensure it can be run in parallel (https://github.com/solo-io/gloo/blob/master/test/services/envoy.go#L401)
  - I believe our current way of running tests only runs a single envoy at a time, so this will always be 1 instance more than the default case, which is why it is port 21001.
- Click on the `config_dump` hyperlink to obtain a complete dump of the current envoy configuration
## (Option B) Run Envoy as a Binary
*Note: We need someone to update these instructions*


## Additional Notes

### Notes on EC2 tests
*Note: these instructions are out of date, and require updating*

- set up your ec2 instance
  - download a simple echo app
  - make the app executable
  - run it in the background

```bash
wget https://mitch-solo-public.s3.amazonaws.com/echoapp2
chmod +x echoapp2
sudo ./echoapp2 --port 80 &
```

### Notes on AWS Lambda Tests (`test/e2e/aws_test.go`)

In addition to the configuration steps provided above, you will need to do the following to run the [AWS Lambda Tests](https://github.com/solo-io/gloo/blob/master/test/e2e/aws_test.go) locally:
  1. Obtain an AWS IAM User account that is part of the Solo.io organization
  2. Create an AWS access key
       - Sign into the AWS console with the account created during step 1
       - Hover over your username at the top right of the page. Click on "My Security Credentials"
       - In the section titled "AWS IAM credentials", click "Create access key" to create an acess key
       - Save the Access key ID and the associated secret key
  3. Install AWS CLI v2
       - You can check whether you have AWS CLI installed by running `aws --version`
       - Installation guides for various operating systems can be found [here](https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html)
  4. Configure AWS credentials on your machine
       - You can do this by running `aws configure`
       - You will be asked to provide your Access Key ID and Secret Key from step 2, as well as the default region name and default output format
         - It is critical that you set the default region to `us-east-1`
       - This will create a credentials file at `~/.aws/credentials` on Linux or macOS, or at `C:\Users\USERNAME\.aws\credentials` on Windows. The tests read this file in order to interact with lambdas that have been created in the Solo.io organization
    