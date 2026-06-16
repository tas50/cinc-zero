# Install the PostgreSQL server and apply config from environment attributes.
package 'postgresql-server'

service 'postgresql' do
  action [:enable, :start]
end
