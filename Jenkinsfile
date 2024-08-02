@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_20_04_label'
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
        sh '''
        echo "Setting up the environment"
        '''
      }
    }
    stage('Push charts') {
      when {
        anyOf {
          branch 'main'
          branch 'hotfix/*'
        }
        expression { !isPrBuild() }
      }
      steps {
        withAWS(region:'us-east-1', credentials:'CICD_HELM') {
          sh """
            make package-charts
            make push-charts
            make build-properties
          """
          archiveArtifacts artifacts: '*.tgz'
          archiveArtifacts artifacts: '*build.properties'
        }
      }
    }
  }
  post {
    success {
      finalizeBuild('', getFileList("*.properties"))
    }
  }
}
