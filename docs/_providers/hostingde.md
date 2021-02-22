---
name: hosting.de
title: hosting.de Provider
layout: default
jsId: HOSTINGDE
---
# hosting.de Provider

## Configuration
In your credentials file, you must provide your API key. One can create an API key in the customer interface under [profile](https://secure.hosting.de/profile). Your API key needs at least the right to update zones.

{% highlight json %}
{
  "hostingde":{
    "apikey": "xxxxx" 
  }
}
{% endhighlight %}

## Usage
Example Javascript:

{% highlight js %}
var REG_NONE = NewRegistrar('none', 'NONE');
var DNS_HOSTINGDE = NewDnsProvider('hostingde', 'HOSTINGDE');

D('dnscontroltest.de', REG_NONE, DnsProvider(DNS_HOSTINGDE),
    A('@', '1.2.3.4'),
    MX('@', 10, 'test123.de.'),
    AAAA('sub', '2a03:2900::1'),
    MX('sub', 10, 'mail')
);
{%endhighlight%}

## Limitations
There are some limitations, because hosting.de supports features which are mainly incompatible with dnscontrol.

- With hosting.de one can managed so called nameserver sets. These are not supported at the moment. Please create your zones without a nameserver set in the expert mode with free defined NS records.
- Also this implementation does not use DNS templates. Please do not tie your zone to a DNS template as this blocks changes to tied records.