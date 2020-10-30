#!/usr/bin/env python3

import sys
import requests

# TODO: Check all return values and assert/bailout on error

url = "http://" + sys.argv[1] + ":8080/api/v1/"
tmpdir = sys.argv[2]

def is_controller_up():
    ret = requests.get(url+"getalltenants")
    return (ret.status_code == 200)

def create_gateway(gw):
    data = {'name': gw}
    ret = requests.post(url+"addgateway", json=data)

def create_tenant(name, gws, image, pods):
    data = {'curid': 'unknown', 'name': name, 'gateways': gws, 
            'image': image, 'pods': pods}
    ret = requests.post(url+"addtenant", json=data)

def get_gws():
    ret = requests.get(url+"getalltenants")
    return ret.json()

def create_user(uid, tenant, name, email, services, gateway):
    data = {'uid': uid, 'tenant': tenant, 'name': name, 'email': email, 'services': services, 'gateway': gateway}
    ret = requests.post(url+"adduser", json=data)

def create_bundle(bid, tenant, name, services, gateway):
    data = {'bid': bid, 'tenant': tenant, 'name': name, 'services': services, 'gateway': gateway}
    ret = requests.post(url+"addbundle", json=data)

def create_user_attr(uid, tenant, category, type, level, dept, team):
    data = {'uid': uid, 'tenant': tenant, 'category': category, 'type': type, 'level': level,
            'dept': dept, 'team': team}
    ret = requests.post(url+"adduserattr", json=data)

def create_bundle_attr(bid, tenant, dept, team, IC, manager, nonemployee):
    data = {'bid': bid, 'tenant': tenant, 'IC': IC, 'manager': manager,
            'nonemployee': nonemployee, 'dept': dept, 'team': team}
    ret = requests.post(url+"addbundleattr", json=data)

def create_policy(tenant, pid, policy):
    rego = []
    for p in policy:
        rego.append(ord(p))
    data = {'tenant': tenant, 'pid': pid, 'rego': rego}
    ret = requests.post(url+"addpolicy", json=data)
    
def create_route(tenant, user, route, tag):
    data = {'tenant': tenant, 'route': user + ":" + route, 'tag': tag}
    ret = requests.post(url+"addroute", json=data)

def create_cert():
    f = open(tmpdir+"/rootca.crt", 'r')
    cert = f.read()
    f.close()
    data = {'certid': 'CACert', 'cert': [ord(c) for c in cert]}
    requests.post(url+"addcert", json=data)

if __name__ == '__main__':
    while not is_controller_up():
        sleep(5)

    create_gateway("gateway.testa.nextensio.net")
    create_gateway("gateway.testc.nextensio.net")

    create_tenant("Test", ["gateway.testa.nextensio.net","gateway.testc.nextensio.net"], 
                  "registry.gitlab.com/nextensio/cluster/minion:latest", 5)

    gws = get_gws()
    tenant = gws[0]['_id']

    create_user('test1@nextensio.net', tenant, 'Test User1', 'test1@nextensio.net', ['test1-nextensio-net'], 'gateway.testa.nextensio.net')
    create_user_attr('test1@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])
    
    create_user('test2@nextensio.net', tenant, 'Test User2', 'test2@nextensio.net', ['test2-nextensio-net'], 'gateway.testa.nextensio.net')
    create_user_attr('test2@nextensio.net', tenant, 'employee', 'IC', 50, ['ABU,BBU'], ['engineering','sales'])

    create_bundle('default@nextensio.net', tenant, 'Default Internet Route', ['default-internet'], 'gateway.testc.nextensio.net')
    create_bundle_attr('default@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    create_bundle('v1.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v1.kismis.org'], 'gateway.testc.nextensio.net')
    create_bundle_attr('v1.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    create_bundle('v2.kismis@nextensio.net', tenant, 'Kismis Version ONE', ['v2.kismis.org'], 'gateway.testc.nextensio.net')
    create_bundle_attr('v2.kismis@nextensio.net', tenant, ['ABU,BBU'], ['engineering','sales'], 1, 1, "allowed")

    create_route(tenant, 'test1@nextensio.net', 'kismis.org', 'v1')
    create_route(tenant, 'test2@nextensio.net', 'kismis.org', 'v2')

    with open('policy.AccessPolicy','r') as file:
        rego = file.read()
    create_policy(tenant, 'AccessPolicy', rego)
    create_cert()
