

<h1 align="center">
    <img src="https://github.com/solo-io/gloo/blob/v2.0.x/docs/content/img/logo-gloo-gateway-horizontal.svg" alt="Gloo Gateway v2" width="800">
  <br> 
  An Envoy-Powered API Gateway
</h1>

## Important Update

> **Important**
> Gloo Gateway is now a fully conformant Kubernetes Gateway API implementation!
>
> The existing Gloo Edge v1 APIs were not changed and continue to be fully supported.

## About Gloo Gateway
Gloo Gateway is a powerful Kubernetes-native ingress controller and API gateway that is based on the Kubernetes Gateway API. It excels in function-level routing, supports legacy apps, microservices and serverless, offers robust discovery capabilities, integrates seamlessly with open-source projects, and is designed to support hybrid applications with various technologies, architectures, protocols, and clouds.

[**Installation**](https://docs.solo.io/gloo-gateway/v2/quickstart) &nbsp; |
&nbsp; [**Documentation**](https://docs.solo.io/gloo-gateway/v2) &nbsp; |
&nbsp; [**Blog**](https://www.solo.io/blog/?category=gloo) &nbsp; |
&nbsp; [**Slack**](https://slack.solo.io) &nbsp; |
&nbsp; [**Twitter**](https://twitter.com/soloio_inc) |
&nbsp; [**Enterprise Trial**](https://www.solo.io/free-trial/)

<BR><center><img src="https://docs.solo.io/gloo-edge/main/img/gloo-architecture-envoys.png" alt="Gloo Gateway v2 Architecture" width="906"></center>

## Quickstart with `glooctl`
Install Gloo Gateway and set up routing to the httpbin sample app. 

1. Install `glooctl`, the Gloo Gateway command line tool.
   ```sh
   curl -sL https://run.solo.io/gloo/install | GLOO_VERSION=v2.0.0-beta1 sh
   export PATH=$HOME/.gloo/bin:$PATH
   ```

2. Install the Gloo Gateway v2 control plane, and wait for it to come up.
   ```sh
   glooctl install
   ```

3. Deploy the httpbin sample app, along with a Gateway and HTTPRoute to access it.
   ```sh
   kubectl -n httpbin apply -f https://raw.githubusercontent.com/solo-io/gloo/v2.0.x/projects/gateway2/examples/httpbin.yaml
   ```

4. Port-forward the Gateway.
   ```sh
   kubectl port-forward deployment/gloo-proxy-http -n httpbin 8080:8080
   ```

5. Send a request through our new Gateway.
   ```sh
   curl -I localhost:8080/status/200 -H "host: www.example.com" -v
   ```

Congratulations! You successfully installed Gloo Gateway and used an HTTP gateway to expose the httpbin sample app. 

> **Note**
> To learn more about Gloo Gateway's support for the Kubernetes Gateway API, see the [docs](https://docs.solo.io/gloo-gateway/v2/).

## Quickstart with Helm 

1. Install the custom resources of the Kubernetes Gateway API. 
   ```sh
   kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.0.0/standard-install.yaml
   ```

2. Install Gloo Gateway v2. This command creates the `gloo-system` namespace and installs the Gloo Gateway v2 control plane into it.
   ```sh
   helm install default -n gloo-system --create-namespace  oci://ghcr.io/solo-io/helm-charts/gloo-gateway --version 2.0.0-beta1
   ```

3. Verify that the Gloo Gateway v2 control plane is up and running and that the `gloo-gateway` GatewayClass is created. 
   ```sh
   kubectl get pods -n gloo-system
   kubectl get gatewayclass gloo-gateway 
   ```

4. Deploy the httpbin sample app, along with a Gateway and HTTPRoute to access it.
   ```sh
   kubectl -n httpbin apply -f https://raw.githubusercontent.com/solo-io/gloo/v2.0.x/projects/gateway2/examples/httpbin.yaml
   ```

5. Port-forward the Gateway.
   ```sh
   kubectl port-forward deployment/gloo-proxy-http -n httpbin 8080:8080
   ```

6. Send a request through our new Gateway.
   ```sh
   curl -I localhost:8080/status/200 -H "host: www.example.com" -v
   ```

> **Note**
> To learn more about Gloo Gateway's support for the Kubernetes Gateway API, see the [docs](https://docs.solo.io/gloo-gateway/v2/).

### Using Gloo Gateway
- **Kubernetes Gateway API**: Gloo Gateway is a feature-rich ingress controller, built on top of the Envoy Proxy and fully conformant with the Kubernetes Gateway API.
- **Next-generation API gateway**: Gloo Gateway provides a long list of API gateway features including rate limiting, circuit breaking, retries, caching, transformation, service-mesh integration, security, external authentication and authorization.
- **Hybrid apps**: Gloo Gateway creates applications that route to backends implemented as microservices, serverless functions and legacy apps. This feature can help users to -
A) Gradually migrate from their legacy code to microservices and serverless.
B) Add new functionalities using cloud-native technologies while maintaining their legacy codebase.
C) Allow different teams in an organization choose different architectures. See here for more on the Hybrid App paradigm.


### What makes Gloo Gateway unique
- **Function-level routing allows integration of legacy applications, microservices and serverless**: Gloo Gateway can route requests directly to functions. Request to Function can be a serverless function call (e.g. Lambda, Google Cloud Function, OpenFaaS Function, etc.), an API call on a microservice or a legacy service (e.g. a REST API call, OpenAPI operation, XML/SOAP request etc.), or publishing to a message queue (e.g. NATS, AMQP, etc.). This unique ability is what makes Gloo Gateway the only API gateway that supports hybrid apps as well as the only one that does not tie the user to a specific paradigm.
- **Gloo Gateway incorporates vetted open-source projects to provide broad functionality**: Gloo Gateway supports high-quality features by integrating with top open-source projects, including gRPC, GraphQL, OpenTracing, NATS and more. Gloo Gateway's architecture allows rapid integration of future popular open-source projects as they emerge.
 **Full automated discovery lets users move fast**: Upon launch, Gloo Gateway creates a catalog of all available destinations and continuously keeps them up to date. This takes the responsibility for 'bookkeeping' away from the developers and guarantees that new features become available as soon as they are ready. Gloo Gateway discovers across IaaS, PaaS and FaaS providers as well as Swagger, gRPC, and GraphQL.


## Next Steps
- Join us on our Slack channel: [https://slack.solo.io/](https://slack.solo.io/)
- Follow us on Twitter: [https://twitter.com/soloio_inc](https://twitter.com/soloio_inc)
- Check out the docs: [https://docs.solo.io/gloo-gateway/v2](https://docs.solo.io/gloo-gateway/v2)
- Check out the code and contribute: [Contribution Guides](/devel/contributing)

## Thanks

**Gloo Gateway** would not be possible without the valuable open-source work of projects in the community. We would like to extend a special thank-you to [Envoy](https://www.envoyproxy.io).


## Security

*Reporting security issues* : We take Gloo Gateway's security very seriously. If you've found a security issue or a potential security issue in Gloo Gateway, please DO NOT file a public Github issue, instead send your report privately to [security@solo.io](mailto:security@solo.io).
