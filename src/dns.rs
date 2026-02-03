use std::collections::HashMap;
use mdns_sd::{ServiceDaemon, ServiceInfo};

pub fn start_responder(server_port: u16) -> ServiceDaemon {
    let mdns = ServiceDaemon::new()
                .expect("Failed to create mDNS daemon");

    let service_type = "_http._tcp.local.";
    let instance_name = "oxidrop";
    let host_name = "oxidrop.local.";

    let my_service = ServiceInfo::new(
        service_type, instance_name, host_name, "", // it will auto detect the IP 
        server_port, HashMap::new(),
    )
    .expect("Failed to create service info")
    .enable_addr_auto();    // Critical: Updates the device if your IP changes

    mdns.register(my_service).expect("Failed to register mDNS");

    mdns  // return value 
}