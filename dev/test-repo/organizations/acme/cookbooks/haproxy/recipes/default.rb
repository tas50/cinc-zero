# Install HAProxy and balance the production web tier.
package 'haproxy'

service 'haproxy' do
  action [:enable, :start]
end
