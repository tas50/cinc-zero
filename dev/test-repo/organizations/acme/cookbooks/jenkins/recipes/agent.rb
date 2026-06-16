# Provision a Jenkins CI agent on the staging ci node.
package 'java-17-openjdk-headless'

service 'jenkins-agent' do
  action [:enable, :start]
end
