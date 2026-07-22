# LAN discovery protocol

[繁體中文](lan-discovery.zh-TW.md)

Clients advertise a NexDrop device identifier, protocol version, and transfer endpoint through mDNS and DNS-SD. They never broadcast tokens, private keys, or content. A receiver authenticates the peer with its paired device public key. Discovery identifies only a route candidate and does not grant authorization. Cross-VLAN routing, access-point isolation, or operating-system background limits can prevent discovery, in which case clients fall back to the Node.
