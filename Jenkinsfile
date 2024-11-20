@Library('jenkins.shared.library') _

pipeline {
  agent {
    label 'ubuntu_20_04_label'
  }
  tools {
    go "Go 1.22.4"
  }
  stages {
    stage("Setup") {
      steps {
        prepareBuild()
        sh '''
        echo "Setting up the environment"
        echo "Jenkins may have left over files from previous builds"
        git clean -df
        '''
        sh 'echo cicd > .id' // Pre-create .id to avoid Makefile execution
      }
    }
    stage('Build') {
      steps {
        sh '''
            make package-charts
            make build-properties
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
        script {
          env.PUSH_IMAGE = true
        }
        withAWS(region:'us-east-1', credentials:'CICD_HELM') {
          sh """
            make push-charts
          """
          archiveArtifacts artifacts: '*.tgz'
          archiveArtifacts artifacts: '*build.properties'
        }
      }
    }
  }
  post {
    success {
      script {
        if (env.PUSH_IMAGE) {
          finalizeBuild('', getFileList("*.properties"))
        }
      }
    }
  }
}
