# Install NGINX and tune workers from environment attributes.
package 'nginx'

service 'nginx' do
  action [:enable, :start]
end
