# Install Redis for the production cache tier.
package 'redis-server'

service 'redis' do
  action [:enable, :start]
end
