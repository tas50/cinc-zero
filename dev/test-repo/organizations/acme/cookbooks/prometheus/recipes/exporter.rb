# Install the Prometheus node exporter on the monitoring node.
package 'prometheus-node-exporter'

service 'node_exporter' do
  action [:enable, :start]
end
