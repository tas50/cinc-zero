# Deploy the ACME application on top of the nginx and postgresql tiers.
include_recipe 'nginx::default'

directory '/srv/app' do
  recursive true
end
