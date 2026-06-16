# Schedule periodic chef-client runs from environment attributes.
service 'chef-client' do
  action [:enable, :start]
end
